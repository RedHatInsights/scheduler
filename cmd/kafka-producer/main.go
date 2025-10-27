package main

import (
	"log"

	"github.com/IBM/sarama"

	"insights-scheduler-part-2/internal/config"
)

func main() {

	topic := "platform.inventory.host-ingress-p1"

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to read config: %v", err)
	}

	log.Printf("[DEBUG] KafkaProducer - initializing with brokers: %v, topic: %s", cfg.Kafka.Brokers, topic)

	saramaConfig := sarama.NewConfig()
	saramaConfig.Producer.RequiredAcks = sarama.WaitForAll // Wait for all replicas to acknowledge
	saramaConfig.Producer.Retry.Max = 5                    // Retry up to 5 times
	saramaConfig.Producer.Return.Successes = true
	saramaConfig.Producer.Compression = sarama.CompressionSnappy // Use snappy compression

	producer, err := sarama.NewSyncProducer(cfg.Kafka.Brokers, saramaConfig)
	if err != nil {
		log.Printf("[DEBUG] KafkaProducer - failed to create producer: %v", err)
		return
	}

	// Marshal the message to JSON
	messageBytes := buildMessage()

	// Create Kafka message
	kafkaMessage := &sarama.ProducerMessage{
		Topic: "platform.inventory.host-ingress-p1",
		Key:   sarama.StringEncoder("000001"),
		Value: sarama.StringEncoder(messageBytes),
	}

	// Send the message
	partition, offset, err := producer.SendMessage(kafkaMessage)
	if err != nil {
		log.Printf("[DEBUG] KafkaProducer - failed to send message: %v", err)
		return
	}

	log.Printf("[DEBUG] KafkaProducer - message sent successfully to partition %d at offset %d", partition, offset)
	return
}

func buildMessage() []byte {
	return []byte(`{"operation": "add_host", "platform_metadata": {"request_id": "test-12345", "b64_identity": "eyJpZGVudGl0eSI6IHsiYWNjb3VudF9udW1iZXIiOiAiMDAwMDAwMiIsICJpbnRlcm5hbCI6IHsib3JnX2lkIjogIjAwMDAwMSJ9LCAidHlwZSI6ICJTeXN0ZW0iLCAib3JnX2lkIjogIjAwMDAwMSIsICJhdXRoX3R5cGUiOiAiY2VydC1hdXRoIiwgInN5c3RlbSI6IHsiY24iOiAiNGRhYzJmZTAtZTM2My00YzYxLTk4ZDItOTExNDgwMmZiZmQzIiwgImNlcnRfdHlwZSI6ICJzeXN0ZW0ifX19"}, "data": {"org_id": "000001", "subscription_manager_id": "edbb6669-7317-4b6a-808d-27918befff50", "display_name": "physical_mock_test-uhfvws.example.com", "fqdn": "physical_mock_test-uhfvws.example.com", "stale_timestamp": "2025-09-19T16:53:03Z", "facts": [{"namespace": "rhsm", "facts": {"orgId": "000001", "MEMORY": 32, "RH_PROD": ["69"], "IS_VIRTUAL": false, "ARCHITECTURE": "x86_64", "SYNC_TIMESTAMP": "2025-09-19T16:53:03Z", "SYSPURPOSE_ADDONS": [], "SYSPURPOSE_SLA": "Premium", "SYSPURPOSE_USAGE": "Production", "dmi.system.uuid": "92e04994-e25c-43e9-bcb2-d569c3cd4ea2"}}], "reporter": "rhsm-conduit", "system_profile": {"arch": "x86_64", "owner_id": "edbb6669-7317-4b6a-808d-27918befff50", "cores_per_socket": 4, "number_of_sockets": 2, "number_of_cpus": 8, "threads_per_core": 2, "infrastructure_type": "physical", "system_memory_bytes": 34359738368}, "insights_id": "16644002-5abd-4399-aa60-a2cd97a121d2", "id": "e77eedac-8e0e-47a4-accf-fc364846d24c"}}`)
}

/*
{"org_id": "000001", "subscription_manager_id": "c2c25a7d-03df-42c3-97b1-8c2b321c255f", "display_name": "virtual_mock_test-qtrgkp.example.com", "fqdn": "virtual_mock_test-qtrgkp.example.com", "facts": [{"namespace": "rhsm", "facts": {"orgId": "000001", "MEMORY": 16, "RH_PROD": ["69"], "IS_VIRTUAL": true, "ARCHITECTURE": "x86_64", "SYNC_TIMESTAMP": "2025-09-19T16:53:03Z", "SYSPURPOSE_ADDONS": [], "SYSPURPOSE_SLA": "Premium", "SYSPURPOSE_USAGE": "Production", "dmi.system.uuid": "7349b74a-e06d-4334-a42b-af0e98cc2c67", "VM_HOST": "hypervisor_dacf522b-0539-46db-9223-0229505cb2da"}}], "reporter": "rhsm-conduit", "system_profile": {"arch": "x86_64", "owner_id": "c2c25a7d-03df-42c3-97b1-8c2b321c255f", "cores_per_socket": 2, "number_of_sockets": 2, "number_of_cpus": 4, "threads_per_core": 1, "infrastructure_type": "virtual", "system_memory_bytes": 17179869184, "virtual_host_uuid": "dacf522b-0539-46db-9223-0229505cb2da"}, "insights_id": "4d4ffa8a-3a05-4012-98e3-4650b9cf7580", "id": "2b08f1af-66c1-43dd-9019-60c4de5db08b"}

{"org_id": "000001", "subscription_manager_id": "912fc9be-1e82-4163-aeea-25185ceb3b9a", "display_name": "openshift_cluster_test-caltpd.example.com", "fqdn": "openshift_cluster_test-caltpd.example.com", "facts": [{"namespace": "rhsm", "facts": {"orgId": "000001", "MEMORY": 128, "RH_PROD": ["290"], "IS_VIRTUAL": true, "ARCHITECTURE": "x86_64", "SYNC_TIMESTAMP": "2025-09-19T16:53:03Z", "SYSPURPOSE_ADDONS": [], "SYSPURPOSE_SLA": "Premium", "SYSPURPOSE_USAGE": "Production", "BILLING_MODEL": "Standard", "SYSPURPOSE_UNITS": "Cores/vCPU", "dmi.system.uuid": "70e561d1-2214-4c33-8d41-3f127420e2d7"}}], "reporter": "rhsm-conduit", "system_profile": {"arch": "x86_64", "owner_id": "912fc9be-1e82-4163-aeea-25185ceb3b9a", "cores_per_socket": 8, "number_of_sockets": 4, "number_of_cpus": 32, "threads_per_core": 2, "infrastructure_type": "virtual", "system_memory_bytes": 137438953472}, "insights_id": "c4105e4b-bf0a-40be-916a-cea53993120f", "id": "e54c4b83-bed3-4fd5-9cc9-e37abeb20c04"}
*/
