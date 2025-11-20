package messaging

import (
	"fmt"
	"log"
	"time"

	"github.com/IBM/sarama"
)

// KafkaProducer is a generic Kafka message producer
type KafkaProducer struct {
	producer sarama.SyncProducer
	topic    string
}

// NewKafkaProducer creates a new generic Kafka producer
func NewKafkaProducer(brokers []string, topic string) (*KafkaProducer, error) {
	log.Printf("[DEBUG] KafkaProducer - initializing with brokers: %v, topic: %s", brokers, topic)

	config := sarama.NewConfig()
	config.Producer.RequiredAcks = sarama.WaitForAll // Wait for all replicas to acknowledge
	config.Producer.Retry.Max = 5                    // Retry up to 5 times
	config.Producer.Return.Successes = true
	config.Producer.Compression = sarama.CompressionSnappy // Use snappy compression

	producer, err := sarama.NewSyncProducer(brokers, config)
	if err != nil {
		log.Printf("[DEBUG] KafkaProducer - failed to create producer: %v", err)
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	log.Printf("[DEBUG] KafkaProducer - producer created successfully")
	return &KafkaProducer{
		producer: producer,
		topic:    topic,
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
