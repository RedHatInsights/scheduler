package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	clowder "github.com/redhatinsights/app-common-go/pkg/api/v1"
)

// Config holds all application configuration
type Config struct {
	// Server configuration (with Clowder integration)
	Server ServerConfig `json:"server"`

	// Database configuration (uses Clowder when available)
	Database DatabaseConfig `json:"database"`

	// Kafka configuration (uses Clowder when available)
	Kafka KafkaConfig `json:"kafka"`

	// Metrics configuration (uses Clowder when available)
	Metrics MetricsConfig `json:"metrics"`

	// Export service configuration
	ExportService ExportServiceConfig `json:"export_service"`

	// BOP service configuration
	Bop BopConfig `json:"bop"`

	UserValidatorImpl         string
	JobCompletionNotifierImpl string
}

// ServerConfig contains HTTP server settings
type ServerConfig struct {
	// Port is the main HTTP server port
	Port int `json:"port"`

	// PrivatePort is the port for internal/admin endpoints
	PrivatePort int `json:"private_port"`

	// Host is the server bind address
	Host string `json:"host"`

	// ReadTimeout for HTTP requests
	ReadTimeout time.Duration `json:"read_timeout"`

	// WriteTimeout for HTTP responses
	WriteTimeout time.Duration `json:"write_timeout"`

	// ShutdownTimeout for graceful shutdown
	ShutdownTimeout time.Duration `json:"shutdown_timeout"`
}

// DatabaseConfig contains database connection settings
type DatabaseConfig struct {
	// Type of database (sqlite, postgres, mysql)
	Type string `json:"type"`

	// Path to SQLite database file
	Path string `json:"path"`

	// Host for external databases (postgres, mysql)
	Host string `json:"host"`

	// Port for external databases
	Port int `json:"port"`

	// Name of the database
	Name string `json:"name"`

	// Username for database authentication
	Username string `json:"username"`

	// Password for database authentication
	Password string `json:"password"`

	// SSLMode for database connections (disable, require, verify-ca, verify-full)
	SSLMode string `json:"ssl_mode"`

	// MaxOpenConnections for connection pooling
	MaxOpenConnections int `json:"max_open_connections"`

	// MaxIdleConnections for connection pooling
	MaxIdleConnections int `json:"max_idle_connections"`

	// ConnectionMaxLifetime for connection recycling
	ConnectionMaxLifetime time.Duration `json:"connection_max_lifetime"`
}

// ConnectionString returns a PostgreSQL connection string
func (d *DatabaseConfig) ConnectionString() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.Username, d.Password, d.Name, d.SSLMode)
}

// KafkaConfig contains Kafka connection settings
type KafkaConfig struct {
	// Enabled indicates if Kafka integration is active
	Enabled bool `json:"enabled"`

	// Brokers is a list of Kafka broker addresses
	Brokers []string `json:"brokers"`

	// Topic for export completion messages
	Topic string `json:"topic"`

	// ClientID for Kafka producer identification
	ClientID string `json:"client_id"`

	// Timeout for Kafka operations
	Timeout time.Duration `json:"timeout"`

	// Retries for failed message sends
	Retries int `json:"retries"`

	// BatchSize for batching messages
	BatchSize int `json:"batch_size"`

	// CompressionType (none, gzip, snappy, lz4, zstd)
	CompressionType string `json:"compression_type"`

	// RequiredAcks (0=no ack, 1=leader ack, -1=all replicas ack)
	RequiredAcks int `json:"required_acks"`

	// SASL configuration for authentication
	SASL SASLConfig `json:"sasl"`

	// TLS configuration
	TLS TLSConfig `json:"tls"`
}

// SASLConfig contains SASL authentication settings
type SASLConfig struct {
	// Enabled indicates if SASL is active
	Enabled bool `json:"enabled"`

	// Mechanism (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)
	Mechanism string `json:"mechanism"`

	// Username for SASL authentication
	Username string `json:"username"`

	// Password for SASL authentication
	Password string `json:"password"`
}

// TLSConfig contains TLS settings
type TLSConfig struct {
	// Enabled indicates if TLS is active
	Enabled bool `json:"enabled"`

	// InsecureSkipVerify skips certificate verification
	InsecureSkipVerify bool `json:"insecure_skip_verify"`

	// CertFile path to client certificate
	CertFile string `json:"cert_file"`

	// KeyFile path to client private key
	KeyFile string `json:"key_file"`

	// CAFile path to CA certificate
	CAFile string `json:"ca_file"`
}

// MetricsConfig contains metrics and monitoring settings
type MetricsConfig struct {
	// Port for metrics endpoint
	Port int `json:"port"`

	// Path for metrics endpoint
	Path string `json:"path"`

	// Enabled indicates if metrics are active
	Enabled bool `json:"enabled"`

	// Namespace for metric names
	Namespace string `json:"namespace"`

	// Subsystem for metric names
	Subsystem string `json:"subsystem"`
}

// ExportServiceConfig contains export service client settings
type ExportServiceConfig struct {
	// BaseURL for the export service API
	BaseURL string `json:"base_url"`

	// Timeout for export service requests
	Timeout time.Duration `json:"timeout"`

	// MaxRetries for failed requests
	MaxRetries int `json:"max_retries"`

	// PollMaxRetries is the maximum number of times to poll for export completion
	PollMaxRetries int `json:"poll_max_retries"`

	// PollInterval is the duration to wait between polling attempts
	PollInterval time.Duration `json:"poll_interval"`
}

// BopConfig contains BOP (Back Office Portal) service settings
type BopConfig struct {
	// BaseURL for the BOP API
	BaseURL string `json:"base_url"`

	// APIToken for BOP authentication
	APIToken string `json:"api_token"`

	// ClientID for BOP client identification
	ClientID string `json:"client_id"`

	// InsightsEnv specifies the environment (dev, stage, prod)
	InsightsEnv string `json:"insights_env"`

	// Enabled indicates if BOP integration is active
	Enabled bool `json:"enabled"`

	EphemeralMode bool `json:"ephemeral_mode"`
}

// LoadConfig loads configuration from app-common-go (Clowder) with fallback to environment variables
func LoadConfig() (*Config, error) {
	var clowderConfig *clowder.AppConfig

	// Try to load Clowder configuration first
	if clowder.IsClowderEnabled() {

		fmt.Println("Clowder enabled!!")

		clowderConfig = clowder.LoadedConfig
		if clowderConfig == nil {
			return nil, fmt.Errorf("failed to load Clowder configuration (nil)")
		}
	}

	config := &Config{}

	// Load server configuration with Clowder integration
	config.Server = loadServerConfig(clowderConfig)

	// Load database configuration with Clowder integration
	config.Database = loadDatabaseConfig(clowderConfig)

	// Load Kafka configuration with Clowder integration
	config.Kafka = loadKafkaConfig(clowderConfig)

	// Load metrics configuration with Clowder integration
	config.Metrics = loadMetricsConfig(clowderConfig)

	// Load export service configuration (no Clowder integration needed)
	config.ExportService = loadExportServiceConfig()

	// Load BOP configuration
	config.Bop = loadBopConfig()

	config.UserValidatorImpl = getEnv("USER_VALIDATOR_IMPL", "bop")
	config.JobCompletionNotifierImpl = getEnv("JOB_COMPLETION_NOTIFIER_IMPL", "notifications")

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// loadServerConfig loads server configuration with Clowder integration
func loadServerConfig(clowderConfig *clowder.AppConfig) ServerConfig {
	// Default values
	port := getEnvAsInt("PORT", 5000)
	privatePort := getEnvAsInt("PRIVATE_PORT", 9090)
	host := getEnv("HOST", "0.0.0.0")

	// Override with Clowder values if available
	if clowderConfig != nil {
		if clowderConfig.PublicPort != nil {
			port = *clowderConfig.PublicPort
		}
		if clowderConfig.PrivatePort != nil {
			privatePort = *clowderConfig.PrivatePort
		}
	}

	return ServerConfig{
		Port:            port,
		PrivatePort:     privatePort,
		Host:            host,
		ReadTimeout:     getEnvAsDuration("READ_TIMEOUT", 30*time.Second),
		WriteTimeout:    getEnvAsDuration("WRITE_TIMEOUT", 30*time.Second),
		ShutdownTimeout: getEnvAsDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
	}
}

// loadDatabaseConfig loads database configuration with Clowder integration
func loadDatabaseConfig(clowderConfig *clowder.AppConfig) DatabaseConfig {
	// Default values
	dbType := getEnv("DB_TYPE", "sqlite")
	dbPath := getEnv("DB_PATH", "./jobs.db")
	host := getEnv("DB_HOST", "localhost")
	port := getEnvAsInt("DB_PORT", 5432)
	name := getEnv("DB_NAME", "insights_scheduler")
	username := getEnv("DB_USERNAME", "")
	password := getEnv("DB_PASSWORD", "")
	sslMode := getEnv("DB_SSL_MODE", "disable")

	// Override with Clowder values if available
	if clowderConfig != nil && clowderConfig.Database != nil {
		dbType = "postgres" // Clowder always provides PostgreSQL
		host = clowderConfig.Database.Hostname
		port = clowderConfig.Database.Port
		name = clowderConfig.Database.Name
		username = clowderConfig.Database.Username
		password = clowderConfig.Database.Password
		sslMode = clowderConfig.Database.SslMode
	}

	return DatabaseConfig{
		Type:                  dbType,
		Path:                  dbPath,
		Host:                  host,
		Port:                  port,
		Name:                  name,
		Username:              username,
		Password:              password,
		SSLMode:               sslMode,
		MaxOpenConnections:    getEnvAsInt("DB_MAX_OPEN_CONNECTIONS", 25),
		MaxIdleConnections:    getEnvAsInt("DB_MAX_IDLE_CONNECTIONS", 5),
		ConnectionMaxLifetime: getEnvAsDuration("DB_CONNECTION_MAX_LIFETIME", 5*time.Minute),
	}
}

// loadKafkaConfig loads Kafka configuration with Clowder integration
func loadKafkaConfig(clowderConfig *clowder.AppConfig) KafkaConfig {
	// Default values from environment
	brokers := getEnvAsStringSlice("KAFKA_BROKERS", []string{})
	topic := getEnv("KAFKA_TOPIC", "platform.notifications.ingress")
	enabled := len(brokers) > 0

	// SASL and TLS config from environment
	saslConfig := SASLConfig{
		Enabled:   getEnvAsBool("KAFKA_SASL_ENABLED", false),
		Mechanism: getEnv("KAFKA_SASL_MECHANISM", "PLAIN"),
		Username:  getEnv("KAFKA_SASL_USERNAME", ""),
		Password:  getEnv("KAFKA_SASL_PASSWORD", ""),
	}

	tlsConfig := TLSConfig{
		Enabled:            getEnvAsBool("KAFKA_TLS_ENABLED", false),
		InsecureSkipVerify: getEnvAsBool("KAFKA_TLS_INSECURE_SKIP_VERIFY", false),
		CertFile:           getEnv("KAFKA_TLS_CERT_FILE", ""),
		KeyFile:            getEnv("KAFKA_TLS_KEY_FILE", ""),
		CAFile:             getEnv("KAFKA_TLS_CA_FILE", ""),
	}

	// Override with Clowder values if available
	if clowderConfig != nil && clowderConfig.Kafka != nil {
		enabled = true
		brokers = []string{}

		// Convert Clowder broker configs to string list
		for _, broker := range clowderConfig.Kafka.Brokers {
			brokers = append(brokers, fmt.Sprintf("%s:%d", broker.Hostname, *broker.Port))
		}

		// Find the export topic from Clowder config
		for _, topicConfig := range clowderConfig.Kafka.Topics {
			if topicConfig.RequestedName == topic || topicConfig.Name == topic {
				topic = topicConfig.Name
				break
			}
		}

		// Configure SASL if available in Clowder
		if len(clowderConfig.Kafka.Brokers) > 0 && clowderConfig.Kafka.Brokers[0].Sasl != nil {
			saslConfig.Enabled = true
			if clowderConfig.Kafka.Brokers[0].Sasl.SaslMechanism != nil {
				saslConfig.Mechanism = *clowderConfig.Kafka.Brokers[0].Sasl.SaslMechanism
			}
			if clowderConfig.Kafka.Brokers[0].Sasl.Username != nil {
				saslConfig.Username = *clowderConfig.Kafka.Brokers[0].Sasl.Username
			}
			if clowderConfig.Kafka.Brokers[0].Sasl.Password != nil {
				saslConfig.Password = *clowderConfig.Kafka.Brokers[0].Sasl.Password
			}
		}
	}

	return KafkaConfig{
		Enabled:         enabled,
		Brokers:         brokers,
		Topic:           topic,
		ClientID:        getEnv("KAFKA_CLIENT_ID", "insights-scheduler"),
		Timeout:         getEnvAsDuration("KAFKA_TIMEOUT", 30*time.Second),
		Retries:         getEnvAsInt("KAFKA_RETRIES", 5),
		BatchSize:       getEnvAsInt("KAFKA_BATCH_SIZE", 100),
		CompressionType: getEnv("KAFKA_COMPRESSION", "snappy"),
		RequiredAcks:    getEnvAsInt("KAFKA_REQUIRED_ACKS", -1),
		SASL:            saslConfig,
		TLS:             tlsConfig,
	}
}

// loadMetricsConfig loads metrics configuration with Clowder integration
func loadMetricsConfig(clowderConfig *clowder.AppConfig) MetricsConfig {
	// Default values
	port := getEnvAsInt("METRICS_PORT", 8080)
	path := getEnv("METRICS_PATH", "/metrics")

	// Override with Clowder values if available
	if clowderConfig != nil {
		port = clowderConfig.MetricsPort
		path = clowderConfig.MetricsPath
	}

	return MetricsConfig{
		Port:      port,
		Path:      path,
		Enabled:   getEnvAsBool("METRICS_ENABLED", true),
		Namespace: getEnv("METRICS_NAMESPACE", "insights"),
		Subsystem: getEnv("METRICS_SUBSYSTEM", "scheduler"),
	}
}

// loadExportServiceConfig loads export service configuration from environment
func loadExportServiceConfig() ExportServiceConfig {
	return ExportServiceConfig{
		BaseURL:        getEnv("EXPORT_SERVICE_URL", "http://export-service-service:8000/api/export/v1"),
		Timeout:        getEnvAsDuration("EXPORT_SERVICE_TIMEOUT", 5*time.Minute),
		MaxRetries:     getEnvAsInt("EXPORT_SERVICE_MAX_RETRIES", 3),
		PollMaxRetries: getEnvAsInt("EXPORT_SERVICE_POLL_MAX_RETRIES", 60),
		PollInterval:   getEnvAsDuration("EXPORT_SERVICE_POLL_INTERVAL", 5*time.Second),
	}
}

// loadBopConfig loads BOP configuration from environment
func loadBopConfig() BopConfig {
	apiToken := getEnv("BOP_API_TOKEN", "")
	enabled := apiToken != "" && getEnvAsBool("BOP_ENABLED", true)

	var baseUrl string
	ephemeralMode := getEnvAsBool("BOP_EPHEMERAL_MODE", false)
	if ephemeralMode {
		namespace, err := getOpenshiftNamespace()
		if err != nil {
			panic("Running in BOP ephemeral mode but cannot determine OpenShift namespace")
		}
		baseUrl = fmt.Sprintf("http://env-%s-mbop.%s.svc.cluster.local:8090", namespace, namespace)
	} else {
		baseUrl = getEnv("BOP_URL", "https://backoffice.apps.ext.spoke.preprod.us-east-1.aws.paas.redhat.com")
	}

	return BopConfig{
		BaseURL:     baseUrl,
		APIToken:    apiToken,
		ClientID:    getEnv("BOP_CLIENT_ID", "insights-scheduler"),
		InsightsEnv: getEnv("BOP_INSIGHTS_ENV", "preprod"),
		Enabled:     enabled,
	}
}

func getOpenshiftNamespace() (string, error) {
	filePath := "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

	contentBytes, err := os.ReadFile(filePath)

	if err != nil {
		return "", fmt.Errorf("failed to read file: %v", err)
	}

	namespace := string(contentBytes)

	return namespace, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Validate server ports
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}
	if c.Metrics.Port < 1 || c.Metrics.Port > 65535 {
		return fmt.Errorf("invalid metrics port: %d", c.Metrics.Port)
	}
	if c.Server.PrivatePort < 1 || c.Server.PrivatePort > 65535 {
		return fmt.Errorf("invalid private port: %d", c.Server.PrivatePort)
	}

	// Validate database configuration
	if c.Database.Type == "" {
		return fmt.Errorf("database type is required")
	}
	if c.Database.Type == "sqlite" && c.Database.Path == "" {
		return fmt.Errorf("database path is required for SQLite")
	}
	if c.Database.Type != "sqlite" && c.Database.Host == "" {
		return fmt.Errorf("database host is required for %s", c.Database.Type)
	}

	// Validate Kafka configuration
	if c.Kafka.Enabled {
		if len(c.Kafka.Brokers) == 0 {
			return fmt.Errorf("kafka brokers are required when kafka is enabled")
		}
		if c.Kafka.Topic == "" {
			return fmt.Errorf("kafka topic is required when kafka is enabled")
		}
	}

	// Validate export service configuration
	if c.ExportService.BaseURL == "" {
		return fmt.Errorf("export service base URL is required")
	}

	// Validate BOP configuration (only if enabled)
	if c.Bop.Enabled {
		if c.Bop.BaseURL == "" {
			return fmt.Errorf("BOP base URL is required when BOP is enabled")
		}
		if c.Bop.APIToken == "" {
			return fmt.Errorf("BOP API token is required when BOP is enabled")
		}
		if c.Bop.ClientID == "" {
			return fmt.Errorf("BOP client ID is required when BOP is enabled")
		}
		if c.Bop.InsightsEnv == "" {
			return fmt.Errorf("BOP insights environment is required when BOP is enabled")
		}
	}

	return nil
}

// Helper functions for environment variable parsing

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func getEnvAsStringSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}
