package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clowder "github.com/redhatinsights/app-common-go/pkg/api/v1"
)

// clearConfigEnvVars unsets all known config env vars to ensure test isolation.
func clearConfigEnvVars(t *testing.T) {
	t.Helper()
	envVars := []string{
		"PORT", "PRIVATE_PORT", "HOST", "READ_TIMEOUT", "WRITE_TIMEOUT", "SHUTDOWN_TIMEOUT",
		"DB_TYPE", "DB_PATH", "DB_HOST", "DB_PORT", "DB_NAME", "DB_USERNAME", "DB_PASSWORD",
		"DB_SSL_MODE", "DB_MAX_OPEN_CONNECTIONS", "DB_MAX_IDLE_CONNECTIONS", "DB_CONNECTION_MAX_LIFETIME",
		"REDIS_ENABLED", "REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD", "REDIS_DB", "REDIS_POOL_SIZE",
		"REDIS_TLS_ENABLED", "REDIS_TLS_INSECURE_SKIP_VERIFY", "REDIS_TLS_CERT_FILE", "REDIS_TLS_KEY_FILE", "REDIS_TLS_CA_FILE",
		"KAFKA_BROKERS", "KAFKA_TOPIC", "KAFKA_SECURITY_PROTOCOL", "KAFKA_CLIENT_ID", "KAFKA_TIMEOUT",
		"KAFKA_RETRIES", "KAFKA_BATCH_SIZE", "KAFKA_COMPRESSION", "KAFKA_REQUIRED_ACKS",
		"KAFKA_SASL_ENABLED", "KAFKA_SASL_MECHANISM", "KAFKA_SASL_USERNAME", "KAFKA_SASL_PASSWORD",
		"KAFKA_TLS_ENABLED", "KAFKA_TLS_INSECURE_SKIP_VERIFY", "KAFKA_TLS_CERT_FILE", "KAFKA_TLS_KEY_FILE", "KAFKA_TLS_CA_FILE",
		"METRICS_PORT", "METRICS_PATH", "METRICS_ENABLED", "METRICS_NAMESPACE", "METRICS_SUBSYSTEM",
		"EXPORT_SERVICE_URL", "EXPORT_SERVICE_PUBLIC_URL", "EXPORT_SERVICE_TIMEOUT",
		"EXPORT_SERVICE_MAX_RETRIES", "EXPORT_SERVICE_POLL_MAX_RETRIES", "EXPORT_SERVICE_POLL_INTERVAL",
		"BOP_URL", "BOP_API_TOKEN", "BOP_CLIENT_ID", "BOP_INSIGHTS_ENV", "BOP_ENABLED", "BOP_EPHEMERAL_MODE",
		"SCHEDULER_GRACEFUL_SHUTDOWN_TIMEOUT", "SCHEDULER_REDIS_POLL_INTERVAL",
		"SCHEDULER_DB_TO_REDIS_SYNC_INTERVAL", "ENABLE_PERIODIC_SYNC",
		"THREESCALE_URL", "THREESCALE_TIMEOUT", "THREESCALE_ENABLED",
		"USER_VALIDATOR_IMPL", "JOB_COMPLETION_NOTIFIER_IMPL",
		"EXPORT_SERVICE_ACCOUNT", "EXPORT_SERVICE_ORG_ID",
	}

	for _, key := range envVars {
		os.Unsetenv(key)
	}
}

func TestLoadConfig(t *testing.T) {
	clearConfigEnvVars(t)

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

	if config.Database.Type != "postgres" {
		t.Errorf("Expected default database type 'postgres', got %s", config.Database.Type)
	}

	if config.Database.Host != "localhost" {
		t.Errorf("Expected default database host 'localhost', got %s", config.Database.Host)
	}

	if config.Kafka.Enabled {
		t.Error("Expected Kafka to be disabled by default")
	}

	if config.Kafka.Topic != "platform.notifications.ingress" {
		t.Errorf("Expected default Kafka topic 'platform.notifications.ingress', got %s", config.Kafka.Topic)
	}

	if config.Server.ReadTimeout != 30*time.Second {
		t.Errorf("Expected default read timeout 30s, got %v", config.Server.ReadTimeout)
	}

	if config.Scheduler.GracefulShutdownTimeout != 30*time.Second {
		t.Errorf("Expected default graceful shutdown timeout 30s, got %v", config.Scheduler.GracefulShutdownTimeout)
	}

	if config.Scheduler.EnablePeriodicSync {
		t.Error("Expected enable_periodic_sync to be false by default")
	}

	if config.UserValidatorImpl != "bop" {
		t.Errorf("Expected default user_validator_impl 'bop', got %s", config.UserValidatorImpl)
	}
}

func TestLoadConfigWithEnvironmentVariables(t *testing.T) {
	clearConfigEnvVars(t)

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
	os.Setenv("ENABLE_PERIODIC_SYNC", "true")
	os.Setenv("USER_VALIDATOR_IMPL", "fake")

	defer clearConfigEnvVars(t)

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

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

	if len(config.Kafka.Brokers) > 0 && config.Kafka.Brokers[0] != "broker1:9092" {
		t.Errorf("Expected first broker 'broker1:9092', got %s", config.Kafka.Brokers[0])
	}

	if config.ExportService.BaseURL != "https://api.example.com" {
		t.Errorf("Expected export service URL 'https://api.example.com', got %s", config.ExportService.BaseURL)
	}

	// Public URL should default to base URL
	if config.ExportService.PublicBaseURL != "https://api.example.com" {
		t.Errorf("Expected export service public URL 'https://api.example.com', got %s", config.ExportService.PublicBaseURL)
	}

	if !config.Scheduler.EnablePeriodicSync {
		t.Error("Expected enable_periodic_sync to be true")
	}

	if config.UserValidatorImpl != "fake" {
		t.Errorf("Expected user_validator_impl 'fake', got %s", config.UserValidatorImpl)
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	clearConfigEnvVars(t)

	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  port: 9000
  private_port: 9191
  host: "127.0.0.1"
  read_timeout: 60s

database:
  type: postgres
  host: db.example.com
  port: 5433
  name: mydb
  username: admin
  password: secret

redis:
  enabled: true
  host: redis.example.com
  port: 6380

kafka:
  brokers:
    - kafka1:9092
    - kafka2:9092
  topic: custom.topic

export_service:
  base_url: https://export.example.com
  public_base_url: https://public.export.example.com
  timeout: 10m

scheduler:
  graceful_shutdown_timeout: 60s
  enable_periodic_sync: true
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Change to the temp directory so viper finds config.yaml
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.Server.Port != 9000 {
		t.Errorf("Expected port 9000 from config file, got %d", config.Server.Port)
	}

	if config.Server.Host != "127.0.0.1" {
		t.Errorf("Expected host '127.0.0.1' from config file, got %s", config.Server.Host)
	}

	if config.Server.ReadTimeout != 60*time.Second {
		t.Errorf("Expected read timeout 60s from config file, got %v", config.Server.ReadTimeout)
	}

	if config.Database.Type != "postgres" {
		t.Errorf("Expected database type 'postgres' from config file, got %s", config.Database.Type)
	}

	if config.Database.Host != "db.example.com" {
		t.Errorf("Expected database host 'db.example.com' from config file, got %s", config.Database.Host)
	}

	if !config.Redis.Enabled {
		t.Error("Expected Redis to be enabled from config file")
	}

	if config.Redis.Host != "redis.example.com" {
		t.Errorf("Expected Redis host 'redis.example.com' from config file, got %s", config.Redis.Host)
	}

	if !config.Kafka.Enabled {
		t.Error("Expected Kafka to be enabled (has brokers)")
	}

	if len(config.Kafka.Brokers) != 2 {
		t.Errorf("Expected 2 Kafka brokers from config file, got %d", len(config.Kafka.Brokers))
	}

	if config.ExportService.PublicBaseURL != "https://public.export.example.com" {
		t.Errorf("Expected public URL from config file, got %s", config.ExportService.PublicBaseURL)
	}

	if config.Scheduler.GracefulShutdownTimeout != 60*time.Second {
		t.Errorf("Expected graceful shutdown timeout 60s from config file, got %v", config.Scheduler.GracefulShutdownTimeout)
	}

	if !config.Scheduler.EnablePeriodicSync {
		t.Error("Expected enable_periodic_sync true from config file")
	}
}

func TestEnvVarsOverrideConfigFile(t *testing.T) {
	clearConfigEnvVars(t)

	// Create a config file with port 9000
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `
server:
  port: 9000
database:
  type: postgres
  host: localhost
  port: 5432
  name: scheduler
  username: insights
  password: insights
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	// Set env var to override config file
	os.Setenv("PORT", "7000")
	defer os.Unsetenv("PORT")

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Env var should take precedence over config file
	if config.Server.Port != 7000 {
		t.Errorf("Expected port 7000 (env var override), got %d", config.Server.Port)
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
			name: "unsupported database type",
			modifyConfig: func(c *Config) {
				c.Database.Type = "sqlite"
			},
			expectError:   true,
			errorContains: "unsupported database type",
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
			config := &Config{
				Server: ServerConfig{
					Port:        5000,
					PrivatePort: 9090,
					Host:        "0.0.0.0",
				},
				Database: DatabaseConfig{
					Type:     "postgres",
					Host:     "localhost",
					Port:     5432,
					Name:     "scheduler",
					Username: "insights",
					Password: "insights",
					SSLMode:  "disable",
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
			}

			tc.modifyConfig(config)

			err := config.Validate()

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected validation error, but got none")
				} else if tc.errorContains != "" && !strings.Contains(err.Error(), tc.errorContains) {
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

func TestClowderIntegration(t *testing.T) {
	clearConfigEnvVars(t)

	// Build a default config (simulates what viper produces)
	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

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

	// Apply Clowder overrides
	applyClowderOverrides(config, mockClowder)

	// Verify server config
	if config.Server.Port != 8080 {
		t.Errorf("Expected server port 8080, got %d", config.Server.Port)
	}
	if config.Server.PrivatePort != 9999 {
		t.Errorf("Expected private port 9999, got %d", config.Server.PrivatePort)
	}

	// Verify database config
	if config.Database.Type != "postgres" {
		t.Errorf("Expected database type 'postgres', got %s", config.Database.Type)
	}
	if config.Database.Host != "postgres.example.com" {
		t.Errorf("Expected database host 'postgres.example.com', got %s", config.Database.Host)
	}
	if config.Database.Username != "clowder_user" {
		t.Errorf("Expected database username 'clowder_user', got %s", config.Database.Username)
	}

	// Verify Kafka config
	if !config.Kafka.Enabled {
		t.Error("Expected Kafka to be enabled with Clowder config")
	}
	if len(config.Kafka.Brokers) != 1 {
		t.Errorf("Expected 1 Kafka broker, got %d", len(config.Kafka.Brokers))
	}
	if config.Kafka.Brokers[0] != "kafka1.example.com:9092" {
		t.Errorf("Expected broker 'kafka1.example.com:9092', got %s", config.Kafka.Brokers[0])
	}
	if config.Kafka.Topic != "platform.notifications.ingress" {
		t.Errorf("Expected topic 'platform.notifications.ingress', got %s", config.Kafka.Topic)
	}
	if !config.Kafka.SASL.Enabled {
		t.Error("Expected SASL to be enabled")
	}
	if config.Kafka.SASL.Username != "kafka_user" {
		t.Errorf("Expected SASL username 'kafka_user', got %s", config.Kafka.SASL.Username)
	}

	// Verify metrics config
	if config.Metrics.Port != 9090 {
		t.Errorf("Expected metrics port 9090, got %d", config.Metrics.Port)
	}
	if config.Metrics.Path != "/prometheus" {
		t.Errorf("Expected metrics path '/prometheus', got %s", config.Metrics.Path)
	}
}

func TestDerivedValues(t *testing.T) {
	t.Run("kafka enabled from brokers", func(t *testing.T) {
		config := &Config{
			Kafka: KafkaConfig{
				Brokers: []string{"broker:9092"},
			},
			ExportService: ExportServiceConfig{
				BaseURL: "http://example.com",
			},
		}
		applyDerivedValues(config)
		if !config.Kafka.Enabled {
			t.Error("Expected Kafka to be enabled when brokers are present")
		}
	})

	t.Run("bop disabled without token", func(t *testing.T) {
		config := &Config{
			Bop: BopConfig{
				Enabled:  true,
				APIToken: "",
			},
			ExportService: ExportServiceConfig{
				BaseURL: "http://example.com",
			},
		}
		applyDerivedValues(config)
		if config.Bop.Enabled {
			t.Error("Expected BOP to be disabled when API token is empty")
		}
	})

	t.Run("export public URL defaults to base URL", func(t *testing.T) {
		config := &Config{
			ExportService: ExportServiceConfig{
				BaseURL:       "http://internal.example.com",
				PublicBaseURL: "",
			},
		}
		applyDerivedValues(config)
		if config.ExportService.PublicBaseURL != "http://internal.example.com" {
			t.Errorf("Expected public URL to default to base URL, got %s", config.ExportService.PublicBaseURL)
		}
	})

	t.Run("export public URL preserved when set", func(t *testing.T) {
		config := &Config{
			ExportService: ExportServiceConfig{
				BaseURL:       "http://internal.example.com",
				PublicBaseURL: "https://public.example.com",
			},
		}
		applyDerivedValues(config)
		if config.ExportService.PublicBaseURL != "https://public.example.com" {
			t.Errorf("Expected public URL to be preserved, got %s", config.ExportService.PublicBaseURL)
		}
	})
}

func TestDurationEnvironmentVariables(t *testing.T) {
	clearConfigEnvVars(t)

	os.Setenv("READ_TIMEOUT", "45s")
	os.Setenv("SCHEDULER_GRACEFUL_SHUTDOWN_TIMEOUT", "2m")
	os.Setenv("EXPORT_SERVICE_TIMEOUT", "10m")

	defer clearConfigEnvVars(t)

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.Server.ReadTimeout != 45*time.Second {
		t.Errorf("Expected read timeout 45s, got %v", config.Server.ReadTimeout)
	}

	if config.Scheduler.GracefulShutdownTimeout != 2*time.Minute {
		t.Errorf("Expected graceful shutdown timeout 2m, got %v", config.Scheduler.GracefulShutdownTimeout)
	}

	if config.ExportService.Timeout != 10*time.Minute {
		t.Errorf("Expected export service timeout 10m, got %v", config.ExportService.Timeout)
	}
}

func TestClowderRedisOverrides(t *testing.T) {
	clearConfigEnvVars(t)

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	sslMode := true
	password := "redis-secret"
	username := "redis-user"
	tlsCAPath := "/tmp/clowder-ca/ca.pem"

	mockClowder := &clowder.AppConfig{
		MetricsPort: 8080,
		MetricsPath: "/metrics",
		InMemoryDb: &clowder.InMemoryDBConfig{
			Hostname: "redis.clowder.svc",
			Port:     6380,
			Password: &password,
			Username: &username,
			SslMode:  &sslMode,
		},
		TlsCAPath: &tlsCAPath,
		Logging:   clowder.LoggingConfig{},
	}

	applyClowderOverrides(config, mockClowder)

	if !config.Redis.Enabled {
		t.Error("Expected Redis to be enabled via Clowder")
	}

	if config.Redis.Host != "redis.clowder.svc" {
		t.Errorf("Expected Redis host 'redis.clowder.svc', got %s", config.Redis.Host)
	}

	if config.Redis.Port != 6380 {
		t.Errorf("Expected Redis port 6380, got %d", config.Redis.Port)
	}

	if config.Redis.Password != "redis-secret" {
		t.Errorf("Expected Redis password 'redis-secret', got %s", config.Redis.Password)
	}

	if !config.Redis.TLS.Enabled {
		t.Error("Expected Redis TLS to be enabled when Clowder SslMode is true")
	}

	if config.Redis.TLS.CAFile != "/tmp/clowder-ca/ca.pem" {
		t.Errorf("Expected Redis TLS CAFile from Clowder TlsCAPath, got %s", config.Redis.TLS.CAFile)
	}
}

func TestClowderRedisOverrides_NoTLS(t *testing.T) {
	clearConfigEnvVars(t)

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	password := "redis-secret"

	mockClowder := &clowder.AppConfig{
		MetricsPort: 8080,
		MetricsPath: "/metrics",
		InMemoryDb: &clowder.InMemoryDBConfig{
			Hostname: "redis.local",
			Port:     6379,
			Password: &password,
		},
		Logging: clowder.LoggingConfig{},
	}

	applyClowderOverrides(config, mockClowder)

	if !config.Redis.Enabled {
		t.Error("Expected Redis to be enabled via Clowder")
	}

	if config.Redis.TLS.Enabled {
		t.Error("Expected Redis TLS to be disabled when Clowder SslMode is nil")
	}
}

func TestClowderRedisOverrides_TLSWithoutCAPath(t *testing.T) {
	clearConfigEnvVars(t)

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	sslMode := true

	mockClowder := &clowder.AppConfig{
		MetricsPort: 8080,
		MetricsPath: "/metrics",
		InMemoryDb: &clowder.InMemoryDBConfig{
			Hostname: "redis.local",
			Port:     6379,
			SslMode:  &sslMode,
		},
		Logging: clowder.LoggingConfig{},
	}

	applyClowderOverrides(config, mockClowder)

	if !config.Redis.TLS.Enabled {
		t.Error("Expected Redis TLS to be enabled when Clowder SslMode is true")
	}

	if config.Redis.TLS.CAFile != "" {
		t.Errorf("Expected empty CAFile when TlsCAPath is nil, got %s", config.Redis.TLS.CAFile)
	}
}

func TestRedisDefaultTLSConfig(t *testing.T) {
	clearConfigEnvVars(t)

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config.Redis.TLS.Enabled {
		t.Error("Expected Redis TLS to be disabled by default")
	}

	if config.Redis.TLS.InsecureSkipVerify {
		t.Error("Expected Redis TLS InsecureSkipVerify to be false by default")
	}

	if config.Redis.TLS.CAFile != "" {
		t.Error("Expected Redis TLS CAFile to be empty by default")
	}

	if config.Redis.TLS.CertFile != "" {
		t.Error("Expected Redis TLS CertFile to be empty by default")
	}

	if config.Redis.TLS.KeyFile != "" {
		t.Error("Expected Redis TLS KeyFile to be empty by default")
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
