package messaging

import (
	"fmt"
	"log"
	"strings"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"insights-scheduler/internal/config"
)

// KafkaProducer is a generic Kafka message producer
type KafkaProducer struct {
	producer *kafka.Producer
	topic    string
}

// NewKafkaProducer creates a new generic Kafka producer using confluent-kafka-go
func NewKafkaProducer(cfg *config.KafkaConfig) (*KafkaProducer, error) {
	log.Printf("[INFO] KafkaProducer - initializing connection")
	log.Printf("[INFO] KafkaProducer - brokers: %v", cfg.Brokers)
	log.Printf("[INFO] KafkaProducer - topic: %s", cfg.Topic)
	log.Printf("[INFO] KafkaProducer - SASL enabled: %t", cfg.SASL.Enabled)
	log.Printf("[INFO] KafkaProducer - TLS enabled: %t", cfg.TLS.Enabled)

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
		// Enhanced logging
		"log_level":              5,                                    // LOG_NOTICE
		"debug":                  "broker,topic,msg,protocol,security", // Enable debug contexts
		"statistics.interval.ms": 60000,                                // Statistics every 60 seconds
	}

	// Determine security protocol
	/*
		securityProtocol := "plaintext"
		if cfg.SASL.Enabled && cfg.TLS.Enabled {
			securityProtocol = "sasl_ssl"
		} else if cfg.SASL.Enabled {
			securityProtocol = "sasl_plaintext"
		} else if cfg.TLS.Enabled {
			securityProtocol = "ssl"
		}
	*/

	securityProtocol := cfg.SecurityProtocol

	log.Printf("[INFO] KafkaProducer - security protocol: %s", securityProtocol)
	configMap["security.protocol"] = securityProtocol

	// Configure SASL if enabled
	if cfg.SASL.Enabled {
		if err := configureSASL(&configMap, cfg.SASL); err != nil {
			log.Printf("[ERROR] KafkaProducer - SASL configuration failed: %v", err)
			return nil, err
		}
	}

	// Configure TLS if enabled
	if cfg.TLS.Enabled {
		if err := configureTLS(&configMap, cfg.TLS); err != nil {
			log.Printf("[ERROR] KafkaProducer - TLS configuration failed: %v", err)
			return nil, err
		}
	}

	// Create producer
	log.Printf("[INFO] KafkaProducer - attempting to connect to Kafka brokers...")
	producer, err := kafka.NewProducer(&configMap)
	if err != nil {
		log.Printf("[ERROR] KafkaProducer - failed to create producer: %v", err)
		log.Printf("[ERROR] KafkaProducer - connection troubleshooting:")
		log.Printf("[ERROR]   - Verify brokers are reachable: %v", cfg.Brokers)
		if cfg.SASL.Enabled {
			log.Printf("[ERROR]   - Verify SASL credentials (mechanism: %s, username: %s)", cfg.SASL.Mechanism, cfg.SASL.Username)
		}
		if cfg.TLS.Enabled {
			log.Printf("[ERROR]   - Verify TLS certificates are valid and trusted")
		}
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	log.Printf("[INFO] KafkaProducer - successfully connected to Kafka cluster")
	log.Printf("[INFO] KafkaProducer - producer ready to send messages to topic: %s", cfg.Topic)

	// Start delivery report handler in background
	kp := &KafkaProducer{
		producer: producer,
		topic:    cfg.Topic,
	}
	go kp.deliveryReportHandler()

	return kp, nil
}

// SendMessage sends a generic message to Kafka with the specified key, value, and headers
func (k *KafkaProducer) SendMessage(key string, value []byte, headers map[string]string) error {
	log.Printf("[DEBUG] KafkaProducer - preparing to send message")
	log.Printf("[DEBUG] KafkaProducer - topic: %s", k.topic)
	log.Printf("[DEBUG] KafkaProducer - message key: %s", key)
	log.Printf("[DEBUG] KafkaProducer - message size: %d bytes", len(value))
	log.Printf("[DEBUG] KafkaProducer - headers count: %d", len(headers))

	// Build Kafka headers
	kafkaHeaders := make([]kafka.Header, 0, len(headers))
	for hKey, hVal := range headers {
		kafkaHeaders = append(kafkaHeaders, kafka.Header{
			Key:   hKey,
			Value: []byte(hVal),
		})
		log.Printf("[DEBUG] KafkaProducer - header: %s=%s", hKey, hVal)
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
	log.Printf("[INFO] KafkaProducer - sending message to topic: %s", k.topic)
	deliveryChan := make(chan kafka.Event, 1)
	err := k.producer.Produce(msg, deliveryChan)
	if err != nil {
		log.Printf("[ERROR] KafkaProducer - failed to produce message: %v", err)
		return fmt.Errorf("failed to produce message: %w", err)
	}

	// Wait for delivery confirmation (synchronous send)
	e := <-deliveryChan
	m := e.(*kafka.Message)

	if m.TopicPartition.Error != nil {
		log.Printf("[ERROR] KafkaProducer - delivery failed for topic %s: %v", k.topic, m.TopicPartition.Error)
		log.Printf("[ERROR] KafkaProducer - troubleshooting:")
		log.Printf("[ERROR]   - Check broker connectivity and authentication")
		log.Printf("[ERROR]   - Verify topic '%s' exists and is writable", k.topic)
		log.Printf("[ERROR]   - Check ACLs if using SASL authentication")
		return fmt.Errorf("delivery failed: %w", m.TopicPartition.Error)
	}

	log.Printf("[INFO] KafkaProducer - message sent successfully")
	log.Printf("[INFO] KafkaProducer - partition: %d, offset: %d", m.TopicPartition.Partition, m.TopicPartition.Offset)
	close(deliveryChan)
	return nil
}

// Close closes the Kafka producer
func (k *KafkaProducer) Close() error {
	log.Printf("[INFO] KafkaProducer - closing producer connection")
	if k.producer != nil {
		// Flush any pending messages (wait up to 10 seconds)
		remaining := k.producer.Flush(10000)
		if remaining > 0 {
			log.Printf("[WARN] KafkaProducer - %d messages were not delivered before close", remaining)
		}
		k.producer.Close()
		log.Printf("[INFO] KafkaProducer - producer closed successfully")
		return nil
	}
	log.Printf("[DEBUG] KafkaProducer - producer already closed")
	return nil
}

// deliveryReportHandler processes delivery reports in the background
func (k *KafkaProducer) deliveryReportHandler() {
	for e := range k.producer.Events() {
		switch ev := e.(type) {
		case *kafka.Message:
			if ev.TopicPartition.Error != nil {
				log.Printf("[ERROR] KafkaProducer - delivery failed: %v", ev.TopicPartition.Error)
			} else {
				log.Printf("[DEBUG] KafkaProducer - delivered message to topic %s [%d] at offset %v",
					*ev.TopicPartition.Topic, ev.TopicPartition.Partition, ev.TopicPartition.Offset)
			}
		case kafka.Error:
			log.Printf("[ERROR] KafkaProducer - error: %v", ev)
		default:
			log.Printf("[DEBUG] KafkaProducer - event: %v", ev)
		}
	}
}

// configureSASL configures SASL authentication for the Kafka producer
func configureSASL(configMap *kafka.ConfigMap, saslCfg config.SASLConfig) error {
	log.Printf("[INFO] KafkaProducer - configuring SASL authentication")
	log.Printf("[INFO] KafkaProducer - SASL mechanism: %s", saslCfg.Mechanism)
	log.Printf("[INFO] KafkaProducer - SASL username: %s", saslCfg.Username)
	log.Printf("[DEBUG] KafkaProducer - SASL password length: %d characters", len(saslCfg.Password))

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

	log.Printf("[INFO] KafkaProducer - SASL authentication configured successfully")
	return nil
}

// configureTLS configures TLS encryption for the Kafka producer
func configureTLS(configMap *kafka.ConfigMap, tlsCfg config.TLSConfig) error {
	log.Printf("[INFO] KafkaProducer - configuring TLS encryption")
	log.Printf("[DEBUG] KafkaProducer - TLS InsecureSkipVerify: %t", tlsCfg.InsecureSkipVerify)

	if tlsCfg.InsecureSkipVerify {
		log.Printf("[WARN] KafkaProducer - TLS certificate verification is DISABLED - not recommended for production")
		(*configMap)["enable.ssl.certificate.verification"] = false
	}

	// Configure CA certificate
	if tlsCfg.CAFile != "" {
		log.Printf("[INFO] KafkaProducer - loading CA certificate from: %s", tlsCfg.CAFile)
		(*configMap)["ssl.ca.location"] = tlsCfg.CAFile
		log.Printf("[INFO] KafkaProducer - CA certificate configured")
	} else {
		log.Printf("[DEBUG] KafkaProducer - no custom CA certificate configured (using system trust store)")
	}

	// Configure client certificate
	if tlsCfg.CertFile != "" && tlsCfg.KeyFile != "" {
		log.Printf("[INFO] KafkaProducer - loading client certificate from: %s", tlsCfg.CertFile)
		log.Printf("[INFO] KafkaProducer - loading client key from: %s", tlsCfg.KeyFile)
		(*configMap)["ssl.certificate.location"] = tlsCfg.CertFile
		(*configMap)["ssl.key.location"] = tlsCfg.KeyFile
		log.Printf("[INFO] KafkaProducer - client certificate configured")
	} else {
		log.Printf("[DEBUG] KafkaProducer - no client certificate configured (using CA trust only)")
	}

	log.Printf("[INFO] KafkaProducer - TLS encryption configured successfully")
	return nil
}
