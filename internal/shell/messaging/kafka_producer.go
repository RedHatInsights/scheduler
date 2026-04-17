package messaging

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/IBM/sarama"
	"insights-scheduler/internal/config"
)

// KafkaProducer is a generic Kafka message producer
type KafkaProducer struct {
	producer sarama.SyncProducer
	topic    string
}

// NewKafkaProducer creates a new generic Kafka producer
func NewKafkaProducer(cfg *config.KafkaConfig) (*KafkaProducer, error) {
	log.Printf("[DEBUG] KafkaProducer - initializing with brokers: %v, topic: %s", cfg.Brokers, cfg.Topic)

	saramaConfig := sarama.NewConfig()
	saramaConfig.Producer.RequiredAcks = sarama.WaitForAll // Wait for all replicas to acknowledge
	saramaConfig.Producer.Retry.Max = 5                    // Retry up to 5 times
	saramaConfig.Producer.Return.Successes = true
	saramaConfig.Producer.Compression = sarama.CompressionSnappy // Use snappy compression

	// Configure SASL authentication if enabled
	if cfg.SASL.Enabled {
		if err := configureSASL(saramaConfig, cfg.SASL); err != nil {
			return nil, err
		}
	}

	// Configure TLS if enabled
	if cfg.TLS.Enabled {
		log.Printf("[DEBUG] KafkaProducer - configuring TLS")
		tlsConfig, err := createTLSConfig(cfg.TLS)
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config: %w", err)
		}
		saramaConfig.Net.TLS.Enable = true
		saramaConfig.Net.TLS.Config = tlsConfig
	}

	producer, err := sarama.NewSyncProducer(cfg.Brokers, saramaConfig)
	if err != nil {
		log.Printf("[DEBUG] KafkaProducer - failed to create producer: %v", err)
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	log.Printf("[DEBUG] KafkaProducer - producer created successfully")
	return &KafkaProducer{
		producer: producer,
		topic:    cfg.Topic,
	}, nil
}

// SendMessage sends a generic message to Kafka with the specified key, value, and headers
func (k *KafkaProducer) SendMessage(key string, value []byte, headers map[string]string) error {
	log.Printf("[DEBUG] KafkaProducer - sending message with key: %s", key)

	// Build Kafka headers from map
	kafkaHeaders := make([]sarama.RecordHeader, 0, len(headers))
	for k, v := range headers {
		kafkaHeaders = append(kafkaHeaders, sarama.RecordHeader{
			Key:   []byte(k),
			Value: []byte(v),
		})
	}

	// Create Kafka message
	kafkaMessage := &sarama.ProducerMessage{
		Topic:     k.topic,
		Key:       sarama.StringEncoder(key),
		Value:     sarama.StringEncoder(value),
		Headers:   kafkaHeaders,
		Timestamp: time.Now(),
	}

	// Send the message
	partition, offset, err := k.producer.SendMessage(kafkaMessage)
	if err != nil {
		log.Printf("[DEBUG] KafkaProducer - failed to send message: %v", err)
		return fmt.Errorf("failed to send message: %w", err)
	}

	log.Printf("[DEBUG] KafkaProducer - message sent successfully to partition %d at offset %d", partition, offset)
	return nil
}

// Close closes the Kafka producer
func (k *KafkaProducer) Close() error {
	log.Printf("[DEBUG] KafkaProducer - closing producer")
	if k.producer != nil {
		return k.producer.Close()
	}
	return nil
}

// configureSASL configures SASL authentication for the Kafka producer
func configureSASL(saramaConfig *sarama.Config, saslCfg config.SASLConfig) error {
	log.Printf("[DEBUG] KafkaProducer - configuring SASL authentication (mechanism: %s)", saslCfg.Mechanism)
	saramaConfig.Net.SASL.Enable = true
	saramaConfig.Net.SASL.User = saslCfg.Username
	saramaConfig.Net.SASL.Password = saslCfg.Password

	// Set SASL mechanism
	switch saslCfg.Mechanism {
	case "PLAIN":
		saramaConfig.Net.SASL.Mechanism = sarama.SASLTypePlaintext
	case "SCRAM-SHA-256":
		saramaConfig.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
		saramaConfig.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
			return &XDGSCRAMClient{HashGeneratorFcn: SHA256}
		}
	case "SCRAM-SHA-512":
		saramaConfig.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
		saramaConfig.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
			return &XDGSCRAMClient{HashGeneratorFcn: SHA512}
		}
	default:
		return fmt.Errorf("unsupported SASL mechanism: %s (supported: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)", saslCfg.Mechanism)
	}

	return nil
}

// createTLSConfig creates a TLS configuration from the provided config
func createTLSConfig(cfg config.TLSConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}

	// Load client certificate if provided
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		log.Printf("[DEBUG] KafkaProducer - loaded client certificate from %s", cfg.CertFile)
	}

	// Load CA certificate if provided
	if cfg.CAFile != "" {
		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
		log.Printf("[DEBUG] KafkaProducer - loaded CA certificate from %s", cfg.CAFile)
	}

	return tlsConfig, nil
}
