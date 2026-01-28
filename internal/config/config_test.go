package config

import (
	"os"
	"testing"
	"time"

	clowder "github.com/redhatinsights/app-common-go/pkg/api/v1"
)

func TestLoadConfig(t *testing.T) {
	// Save original environment
	originalEnv := make(map[string]string)
	envVars := []string{
		"PORT", "PRIVATE_PORT", "METRICS_PORT", "DB_TYPE", "DB_PATH",
		"KAFKA_BROKERS", "KAFKA_TOPIC", "EXPORT_SERVICE_URL",
		"EXPORT_SERVICE_ACCOUNT", "EXPORT_SERVICE_ORG_ID",
	}

	for _, key := range envVars {
		originalEnv[key] = os.Getenv(key)
		os.Unsetenv(key)
	}

	// Restore environment after test
	defer func() {
		for key, value := range originalEnv {
			if value != "" {
				os.Setenv(key, value)
			} else {
				os.Unsetenv(key)
			}
		}
	}()

	// Test default configuration
	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify default values
	if config.Server.Port != 5000 {
		t.Errorf("Expected default port 5000, got %d", config.Server.Port)
	}

	if config.Server.PrivatePort != 9090 {
		t.Errorf("Expected default private port 9090, got %d", config.Server.PrivatePort)
	}

	if config.Metrics.Port != 8080 {
		t.Errorf("Expected default metrics port 8080, got %d", config.Metrics.Port)
	}

	if config.Database.Type != "sqlite" {
		t.Errorf("Expected default database type 'sqlite', got %s", config.Database.Type)
	}

	if config.Database.Path != "./jobs.db" {
		t.Errorf("Expected default database path './jobs.db', got %s", config.Database.Path)
	}

	if config.Kafka.Enabled {
		t.Error("Expected Kafka to be disabled by default")
	}

	if config.Kafka.Topic != "platform.notifications.ingress" {
		t.Errorf("Expected default Kafka topic 'platform.notifications.ingress', got %s", config.Kafka.Topic)
	}
}

func TestLoadConfigWithEnvironmentVariables(t *testing.T) {
	// Set environment variables
	os.Setenv("PORT", "8000")
	os.Setenv("PRIVATE_PORT", "9999")
	os.Setenv("METRICS_PORT", "7777")
	os.Setenv("DB_TYPE", "postgres")
	os.Setenv("DB_HOST", "localhost")
	os.Setenv("DB_PORT", "5432")
	os.Setenv("DB_NAME", "test_db")
	os.Setenv("KAFKA_BROKERS", "broker1:9092,broker2:9092")
	os.Setenv("KAFKA_TOPIC", "platform.notifications.ingress")
	os.Setenv("EXPORT_SERVICE_URL", "https://api.example.com")
	os.Setenv("EXPORT_SERVICE_ACCOUNT", "123456")
	os.Setenv("EXPORT_SERVICE_ORG_ID", "org-123")

	defer func() {
		// Clean up
		envVars := []string{
			"PORT", "PRIVATE_PORT", "METRICS_PORT", "DB_TYPE", "DB_HOST", "DB_PORT", "DB_NAME",
			"KAFKA_BROKERS", "KAFKA_TOPIC", "EXPORT_SERVICE_URL", "EXPORT_SERVICE_ACCOUNT", "EXPORT_SERVICE_ORG_ID",
		}
		for _, key := range envVars {
			os.Unsetenv(key)
		}
	}()

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify environment values were loaded
	if config.Server.Port != 8000 {
		t.Errorf("Expected port 8000, got %d", config.Server.Port)
	}

	if config.Server.PrivatePort != 9999 {
		t.Errorf("Expected private port 9999, got %d", config.Server.PrivatePort)
	}

	if config.Metrics.Port != 7777 {
		t.Errorf("Expected metrics port 7777, got %d", config.Metrics.Port)
	}

	if config.Database.Type != "postgres" {
		t.Errorf("Expected database type 'postgres', got %s", config.Database.Type)
	}

	if config.Database.Host != "localhost" {
		t.Errorf("Expected database host 'localhost', got %s", config.Database.Host)
	}

	if !config.Kafka.Enabled {
		t.Error("Expected Kafka to be enabled when brokers are set")
	}

	if len(config.Kafka.Brokers) != 2 {
		t.Errorf("Expected 2 Kafka brokers, got %d", len(config.Kafka.Brokers))
	}

	if config.Kafka.Brokers[0] != "broker1:9092" {
		t.Errorf("Expected first broker 'broker1:9092', got %s", config.Kafka.Brokers[0])
	}

	if config.ExportService.BaseURL != "https://api.example.com" {
		t.Errorf("Expected export service URL 'https://api.example.com', got %s", config.ExportService.BaseURL)
	}
}

func TestConfigValidation(t *testing.T) {
	testCases := []struct {
		name          string
		modifyConfig  func(*Config)
		expectError   bool
		errorContains string
	}{
		{
			name:         "valid config",
			modifyConfig: func(c *Config) {},
			expectError:  false,
		},
		{
			name: "invalid server port",
			modifyConfig: func(c *Config) {
				c.Server.Port = 0
			},
			expectError:   true,
			errorContains: "invalid server port",
		},
		{
			name: "invalid metrics port",
			modifyConfig: func(c *Config) {
				c.Metrics.Port = 70000
			},
			expectError:   true,
			errorContains: "invalid metrics port",
		},
		{
			name: "invalid private port",
			modifyConfig: func(c *Config) {
				c.Server.PrivatePort = -1
			},
			expectError:   true,
			errorContains: "invalid private port",
		},
		{
			name: "empty database type",
			modifyConfig: func(c *Config) {
				c.Database.Type = ""
			},
			expectError:   true,
			errorContains: "database type is required",
		},
		{
			name: "sqlite without path",
			modifyConfig: func(c *Config) {
				c.Database.Type = "sqlite"
				c.Database.Path = ""
			},
			expectError:   true,
			errorContains: "database path is required",
		},
		{
			name: "postgres without host",
			modifyConfig: func(c *Config) {
				c.Database.Type = "postgres"
				c.Database.Host = ""
			},
			expectError:   true,
			errorContains: "database host is required",
		},
		{
			name: "kafka enabled without brokers",
			modifyConfig: func(c *Config) {
				c.Kafka.Enabled = true
				c.Kafka.Brokers = []string{}
			},
			expectError:   true,
			errorContains: "kafka brokers are required",
		},
		{
			name: "kafka enabled without topic",
			modifyConfig: func(c *Config) {
				c.Kafka.Enabled = true
				c.Kafka.Brokers = []string{"broker:9092"}
				c.Kafka.Topic = ""
			},
			expectError:   true,
			errorContains: "kafka topic is required",
		},
		{
			name: "empty export service URL",
			modifyConfig: func(c *Config) {
				c.ExportService.BaseURL = ""
			},
			expectError:   true,
			errorContains: "export service base URL is required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a valid base config
			config := &Config{
				Server: ServerConfig{
					Port:        5000,
					PrivatePort: 9090,
					Host:        "0.0.0.0",
				},
				Database: DatabaseConfig{
					Type: "sqlite",
					Path: "./test.db",
				},
				Kafka: KafkaConfig{
					Enabled: false,
					Topic:   "platform.notifications.ingress",
					Brokers: []string{"broker:9092"},
				},
				Metrics: MetricsConfig{
					Port:    8080,
					Enabled: true,
				},
				ExportService: ExportServiceConfig{
					BaseURL: "http://localhost:9000",
				},
				InventoryPdfService: InventoryPdfServiceConfig{
					BaseURL: "http://pdf-generator:8000",
				},
			}

			// Apply test modification
			tc.modifyConfig(config)

			// Validate
			err := config.Validate()

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected validation error, but got none")
				} else if tc.errorContains != "" && !contains(err.Error(), tc.errorContains) {
					t.Errorf("Expected error to contain '%s', but got: %v", tc.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no validation error, but got: %v", err)
				}
			}
		})
	}
}

func TestEnvironmentVariableParsing(t *testing.T) {
	// Test duration parsing
	os.Setenv("TEST_DURATION", "30s")
	duration := getEnvAsDuration("TEST_DURATION", 1*time.Minute)
	if duration != 30*time.Second {
		t.Errorf("Expected 30s, got %v", duration)
	}
	os.Unsetenv("TEST_DURATION")

	// Test bool parsing
	os.Setenv("TEST_BOOL", "true")
	boolVal := getEnvAsBool("TEST_BOOL", false)
	if !boolVal {
		t.Error("Expected true, got false")
	}
	os.Unsetenv("TEST_BOOL")

	// Test string slice parsing
	os.Setenv("TEST_SLICE", "item1,item2,item3")
	slice := getEnvAsStringSlice("TEST_SLICE", []string{})
	if len(slice) != 3 || slice[0] != "item1" || slice[1] != "item2" || slice[2] != "item3" {
		t.Errorf("Expected [item1, item2, item3], got %v", slice)
	}
	os.Unsetenv("TEST_SLICE")
}

func TestClowderIntegration(t *testing.T) {
	// Mock Clowder configuration
	port := 8080
	privatePort := 9999
	mockClowder := &clowder.AppConfig{
		PublicPort:  &port,
		PrivatePort: &privatePort,
		MetricsPort: 9090,
		MetricsPath: "/prometheus",
		Database: &clowder.DatabaseConfig{
			Hostname: "postgres.example.com",
			Port:     5432,
			Name:     "clowder_db",
			Username: "clowder_user",
			Password: "clowder_pass",
			SslMode:  "require",
		},
		Kafka: &clowder.KafkaConfig{
			Brokers: []clowder.BrokerConfig{
				{
					Hostname: "kafka1.example.com",
					Port:     intPtr(9092),
					Sasl: &clowder.KafkaSASLConfig{
						SaslMechanism: stringPtr("SCRAM-SHA-512"),
						Username:      stringPtr("kafka_user"),
						Password:      stringPtr("kafka_pass"),
					},
				},
			},
			Topics: []clowder.TopicConfig{
				{
					Name:          "platform.notifications.ingress",
					RequestedName: "platform.notifications.ingress",
				},
			},
		},
	}

	// Test server config with Clowder
	serverConfig := loadServerConfig(mockClowder)
	if serverConfig.Port != 8080 {
		t.Errorf("Expected server port 8080, got %d", serverConfig.Port)
	}
	if serverConfig.PrivatePort != 9999 {
		t.Errorf("Expected private port 9999, got %d", serverConfig.PrivatePort)
	}

	// Test database config with Clowder
	dbConfig := loadDatabaseConfig(mockClowder)
	if dbConfig.Type != "postgres" {
		t.Errorf("Expected database type 'postgres', got %s", dbConfig.Type)
	}
	if dbConfig.Host != "postgres.example.com" {
		t.Errorf("Expected database host 'postgres.example.com', got %s", dbConfig.Host)
	}
	if dbConfig.Username != "clowder_user" {
		t.Errorf("Expected database username 'clowder_user', got %s", dbConfig.Username)
	}

	// Test Kafka config with Clowder
	kafkaConfig := loadKafkaConfig(mockClowder)
	if !kafkaConfig.Enabled {
		t.Error("Expected Kafka to be enabled with Clowder config")
	}
	if len(kafkaConfig.Brokers) != 1 {
		t.Errorf("Expected 1 Kafka broker, got %d", len(kafkaConfig.Brokers))
	}
	if kafkaConfig.Brokers[0] != "kafka1.example.com:9092" {
		t.Errorf("Expected broker 'kafka1.example.com:9092', got %s", kafkaConfig.Brokers[0])
	}
	if kafkaConfig.Topic != "platform.notifications.ingress" {
		t.Errorf("Expected topic 'platform.notifications.ingress', got %s", kafkaConfig.Topic)
	}
	if !kafkaConfig.SASL.Enabled {
		t.Error("Expected SASL to be enabled")
	}
	if kafkaConfig.SASL.Username != "kafka_user" {
		t.Errorf("Expected SASL username 'kafka_user', got %s", kafkaConfig.SASL.Username)
	}

	// Test metrics config with Clowder
	metricsConfig := loadMetricsConfig(mockClowder)
	if metricsConfig.Port != 9090 {
		t.Errorf("Expected metrics port 9090, got %d", metricsConfig.Port)
	}
	if metricsConfig.Path != "/prometheus" {
		t.Errorf("Expected metrics path '/prometheus', got %s", metricsConfig.Path)
	}
}

// Helper function for creating int pointers
func intPtr(i int) *int {
	return &i
}

// Helper function for creating string pointers
func stringPtr(s string) *string {
	return &s
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(substr) <= len(s) && s[len(s)-len(substr):] == substr) ||
		(len(substr) <= len(s) && s[:len(substr)] == substr) ||
		(len(substr) < len(s) && hasSubstring(s, substr)))
}

func hasSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
