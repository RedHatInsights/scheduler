package messaging

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/IBM/sarama"
)

type KafkaProducer struct {
	producer sarama.SyncProducer
	topic    string
}

type ExportCompletionMessage struct {
	ExportID    string    `json:"export_id"`
	JobID       string    `json:"job_id"`
	OrgID       string    `json:"org_id"`
	Status      string    `json:"status"`
	CompletedAt time.Time `json:"completed_at"`
	DownloadURL string    `json:"download_url,omitempty"`
	ErrorMsg    string    `json:"error_message,omitempty"`
}

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

func (k *KafkaProducer) SendExportCompletionMessage(message ExportCompletionMessage) error {
	log.Printf("[DEBUG] KafkaProducer - sending export completion message for export: %s", message.ExportID)

	// Marshal the message to JSON
	messageBytes, err := json.Marshal(message)
	if err != nil {
		log.Printf("[DEBUG] KafkaProducer - failed to marshal message: %v", err)
		return fmt.Errorf("failed to marshal export completion message: %w", err)
	}

	// Create Kafka message
	kafkaMessage := &sarama.ProducerMessage{
		Topic: k.topic,
		Key:   sarama.StringEncoder(message.ExportID), // Use export ID as key for partitioning
		Value: sarama.StringEncoder(messageBytes),
		Headers: []sarama.RecordHeader{
			{
				Key:   []byte("event_type"),
				Value: []byte("export_completion"),
			},
			{
				Key:   []byte("org_id"),
				Value: []byte(message.OrgID),
			},
		},
		Timestamp: time.Now(),
	}

	// Send the message
	partition, offset, err := k.producer.SendMessage(kafkaMessage)
	if err != nil {
		log.Printf("[DEBUG] KafkaProducer - failed to send message: %v", err)
		return fmt.Errorf("failed to send export completion message: %w", err)
	}

	log.Printf("[DEBUG] KafkaProducer - message sent successfully to partition %d at offset %d", partition, offset)
	return nil
}

// SendNotificationMessage sends a platform notification message to Kafka
func (k *KafkaProducer) SendNotificationMessage(message *NotificationMessage) error {
	log.Printf("[DEBUG] KafkaProducer - sending notification message for event: %s", message.EventType)

	// Marshal the message to JSON
	messageBytes, err := message.ToJSON()
	if err != nil {
		log.Printf("[DEBUG] KafkaProducer - failed to marshal notification message: %v", err)
		return fmt.Errorf("failed to marshal notification message: %w", err)
	}

	// Create Kafka message
	kafkaMessage := &sarama.ProducerMessage{
		Topic: k.topic,
		Key:   sarama.StringEncoder(message.OrgID), // Use org ID as key for partitioning
		Value: sarama.StringEncoder(messageBytes),
		Headers: []sarama.RecordHeader{
			{
				Key:   []byte("message-type"),
				Value: []byte("platform-notification"),
			},
			{
				Key:   []byte("bundle"),
				Value: []byte(message.Bundle),
			},
			{
				Key:   []byte("application"),
				Value: []byte(message.Application),
			},
			{
				Key:   []byte("event-type"),
				Value: []byte(message.EventType),
			},
			{
				Key:   []byte("org-id"),
				Value: []byte(message.OrgID),
			},
			{
				Key:   []byte("account-id"),
				Value: []byte(message.AccountID),
			},
			{
				Key:   []byte("version"),
				Value: []byte(message.Version),
			},
		},
		Timestamp: time.Now(),
	}

	// Send the message
	partition, offset, err := k.producer.SendMessage(kafkaMessage)
	if err != nil {
		log.Printf("[DEBUG] KafkaProducer - failed to send notification message: %v", err)
		return fmt.Errorf("failed to send notification message: %w", err)
	}

	log.Printf("[DEBUG] KafkaProducer - notification message sent successfully to partition %d at offset %d", partition, offset)
	return nil
}

func (k *KafkaProducer) Close() error {
	log.Printf("[DEBUG] KafkaProducer - closing producer")
	if k.producer != nil {
		return k.producer.Close()
	}
	return nil
}
