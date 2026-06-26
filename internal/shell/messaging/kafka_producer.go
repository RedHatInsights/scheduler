package messaging

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"insights-scheduler/internal/config"
)

// KafkaProducer is a generic Kafka message producer
type KafkaProducer struct {
	producer *kafka.Producer
	topic    string
	logger   *slog.Logger
}

// NewKafkaProducer creates a new generic Kafka producer using confluent-kafka-go
func NewKafkaProducer(cfg *config.KafkaConfig, logger *slog.Logger) (*KafkaProducer, error) {
	logger.Info("Initializing Kafka producer connection")
	logger.Info("Kafka configuration",
		slog.Any("brokers", cfg.Brokers),
		slog.String("topic", cfg.Topic),
		slog.Bool("sasl_enabled", cfg.SASL.Enabled),
		slog.Bool("tls_enabled", cfg.TLS.Enabled))

	// Build ConfigMap for confluent-kafka-go
	configMap := kafka.ConfigMap{
		"bootstrap.servers":  strings.Join(cfg.Brokers, ","),
		"client.id":          "insights-scheduler",
		"acks":               "all", // Wait for all replicas
		"retries":            5,
		"compression.type":   "none",
		"linger.ms":          10,
		"batch.size":         16384,
		"enable.idempotence": true,
		// Kafka client library logging - only errors
		"log_level": 3, // LOG_ERR - only log errors from librdkafka
	}

	securityProtocol := cfg.SecurityProtocol

	logger.Info("Kafka security protocol", slog.String("protocol", securityProtocol))
	if strings.TrimSpace(securityProtocol) != "" {
		configMap["security.protocol"] = securityProtocol
	}

	// Configure SASL if enabled
	if cfg.SASL.Enabled {
		if err := configureSASL(&configMap, cfg.SASL, logger); err != nil {
			logger.Error("SASL configuration failed", slog.Any("error", err))
			return nil, err
		}
	}

	// Configure TLS if enabled
	if cfg.TLS.Enabled {
		if err := configureTLS(&configMap, cfg.TLS, logger); err != nil {
			logger.Error("TLS configuration failed", slog.Any("error", err))
			return nil, err
		}
	}

	// Create producer
	logger.Info("Attempting to connect to Kafka brokers")
	producer, err := kafka.NewProducer(&configMap)
	if err != nil {
		logger.Error("Failed to create Kafka producer", slog.Any("error", err))
		logger.Error("Connection troubleshooting",
			slog.Any("brokers", cfg.Brokers),
			slog.String("sasl_mechanism", cfg.SASL.Mechanism),
			slog.Bool("tls_enabled", cfg.TLS.Enabled))
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	logger.Info("Successfully connected to Kafka cluster")
	logger.Info("Kafka producer ready", slog.String("topic", cfg.Topic))

	// Start delivery report handler in background
	kp := &KafkaProducer{
		producer: producer,
		topic:    cfg.Topic,
		logger:   logger,
	}
	go kp.deliveryReportHandler()

	return kp, nil
}

// SendMessage sends a generic message to Kafka with the specified key, value, and headers
func (k *KafkaProducer) SendMessage(key string, value []byte, headers map[string]string) error {
	k.logger.Debug("Preparing to send Kafka message",
		slog.String("topic", k.topic),
		slog.String("key", key),
		slog.Int("message_size_bytes", len(value)),
		slog.Int("headers_count", len(headers)))

	// Build Kafka headers
	kafkaHeaders := make([]kafka.Header, 0, len(headers))
	for hKey, hVal := range headers {
		kafkaHeaders = append(kafkaHeaders, kafka.Header{
			Key:   hKey,
			Value: []byte(hVal),
		})
		k.logger.Debug("Kafka header", slog.String("key", hKey), slog.String("value", hVal))
	}

	// Create message
	msg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     &k.topic,
			Partition: kafka.PartitionAny,
		},
		Key:     []byte(key),
		Value:   value,
		Headers: kafkaHeaders,
	}

	// Send message (async with delivery channel)
	k.logger.Info("Sending message to Kafka", slog.String("topic", k.topic))
	deliveryChan := make(chan kafka.Event, 1)
	err := k.producer.Produce(msg, deliveryChan)
	if err != nil {
		k.logger.Error("Failed to produce message", slog.Any("error", err))
		return fmt.Errorf("failed to produce message: %w", err)
	}

	// Wait for delivery confirmation (synchronous send)
	e := <-deliveryChan
	m := e.(*kafka.Message)

	if m.TopicPartition.Error != nil {
		k.logger.Error("Kafka message delivery failed",
			slog.String("topic", k.topic),
			slog.Any("error", m.TopicPartition.Error))
		k.logger.Error("Kafka troubleshooting suggestions",
			slog.String("suggestion_1", "Check broker connectivity and authentication"),
			slog.String("suggestion_2", "Verify topic exists and is writable"),
			slog.String("suggestion_3", "Check ACLs if using SASL authentication"))
		return fmt.Errorf("delivery failed: %w", m.TopicPartition.Error)
	}

	k.logger.Info("Message sent successfully",
		slog.Int("partition", int(m.TopicPartition.Partition)),
		slog.Int64("offset", int64(m.TopicPartition.Offset)))
	close(deliveryChan)
	return nil
}

// Close closes the Kafka producer
func (k *KafkaProducer) Close() error {
	k.logger.Info("Closing Kafka producer connection")
	if k.producer != nil {
		// Flush any pending messages (wait up to 10 seconds)
		remaining := k.producer.Flush(10000)
		if remaining > 0 {
			k.logger.Warn("Messages not delivered before close", slog.Int("remaining", remaining))
		}
		k.producer.Close()
		k.logger.Info("Kafka producer closed successfully")
		return nil
	}
	k.logger.Debug("Kafka producer already closed")
	return nil
}

// deliveryReportHandler processes delivery reports in the background
func (k *KafkaProducer) deliveryReportHandler() {
	for e := range k.producer.Events() {
		switch ev := e.(type) {
		case *kafka.Message:
			if ev.TopicPartition.Error != nil {
				k.logger.Error("Kafka delivery failed", slog.Any("error", ev.TopicPartition.Error))
			} else {
				k.logger.Debug("Kafka message delivered",
					slog.String("topic", *ev.TopicPartition.Topic),
					slog.Int("partition", int(ev.TopicPartition.Partition)),
					slog.Int64("offset", int64(ev.TopicPartition.Offset)))
			}
		case kafka.Error:
			k.logger.Error("Kafka error", slog.Any("error", ev))
		default:
			//k.logger.Trace("Kafka event", slog.Any("event", ev))
		}
	}
}

// configureSASL configures SASL authentication for the Kafka producer
func configureSASL(configMap *kafka.ConfigMap, saslCfg config.SASLConfig, logger *slog.Logger) error {
	logger.Info("Configuring SASL authentication",
		slog.String("mechanism", saslCfg.Mechanism),
		slog.String("username", saslCfg.Username))
	logger.Debug("SASL password length", slog.Int("length", len(saslCfg.Password)))

	// Set SASL mechanism
	mechanism := saslCfg.Mechanism
	if mechanism == "" {
		mechanism = "PLAIN"
	}

	// confluent-kafka-go expects lowercase mechanism names
	mechanismLower := strings.ToUpper(saslCfg.Mechanism)
	if mechanismLower == "SCRAM-SHA-256" || mechanismLower == "SCRAM-SHA-512" {
		// Keep the hyphenated form for SCRAM
		mechanism = mechanismLower
	}

	(*configMap)["sasl.mechanism"] = mechanism
	(*configMap)["sasl.username"] = saslCfg.Username
	(*configMap)["sasl.password"] = saslCfg.Password

	logger.Info("SASL authentication configured successfully")
	return nil
}

// configureTLS configures TLS encryption for the Kafka producer
func configureTLS(configMap *kafka.ConfigMap, tlsCfg config.TLSConfig, logger *slog.Logger) error {
	logger.Info("Configuring TLS encryption")
	logger.Debug("TLS configuration", slog.Bool("insecure_skip_verify", tlsCfg.InsecureSkipVerify))

	if tlsCfg.InsecureSkipVerify {
		logger.Warn("TLS certificate verification is DISABLED - not recommended for production")
		(*configMap)["enable.ssl.certificate.verification"] = false
	}

	// Configure CA certificate
	if tlsCfg.CAFile != "" {
		logger.Info("Loading CA certificate", slog.String("ca_file", tlsCfg.CAFile))
		(*configMap)["ssl.ca.location"] = tlsCfg.CAFile
		logger.Info("CA certificate configured")
	} else {
		logger.Debug("No custom CA certificate configured (using system trust store)")
	}

	// Configure client certificate
	if tlsCfg.CertFile != "" && tlsCfg.KeyFile != "" {
		logger.Info("Loading client certificate",
			slog.String("cert_file", tlsCfg.CertFile),
			slog.String("key_file", tlsCfg.KeyFile))
		(*configMap)["ssl.certificate.location"] = tlsCfg.CertFile
		(*configMap)["ssl.key.location"] = tlsCfg.KeyFile
		logger.Info("Client certificate configured")
	} else {
		logger.Debug("No client certificate configured (using CA trust only)")
	}

	logger.Info("TLS encryption configured successfully")
	return nil
}
