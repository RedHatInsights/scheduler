package config

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"

	clowder "github.com/redhatinsights/app-common-go/pkg/api/v1"
)

// Config holds all application configuration.
//
// Configuration is loaded in priority order (highest wins):
//  1. Clowder (production deployment platform)
//  2. Environment variables
//  3. Config file (config.yaml in . or /etc/scheduler/)
//  4. Defaults
type Config struct {
	Server        ServerConfig        `mapstructure:"server" json:"server"`
	Database      DatabaseConfig      `mapstructure:"database" json:"database"`
	Redis         RedisConfig         `mapstructure:"redis" json:"redis"`
	Kafka         KafkaConfig         `mapstructure:"kafka" json:"kafka"`
	Metrics       MetricsConfig       `mapstructure:"metrics" json:"metrics"`
	ExportService ExportServiceConfig `mapstructure:"export_service" json:"export_service"`
	Bop           BopConfig           `mapstructure:"bop" json:"bop"`
	Scheduler     SchedulerConfig     `mapstructure:"scheduler" json:"scheduler"`
	ThreeScale    ThreeScaleConfig    `mapstructure:"threescale" json:"threescale"`

	UserValidatorImpl         string           `mapstructure:"user_validator_impl" json:"user_validator_impl"`
	JobCompletionNotifierImpl string           `mapstructure:"job_completion_notifier_impl" json:"job_completion_notifier_impl"`
	CloudWatch                CloudWatchConfig `mapstructure:"cloudwatch" json:"cloudwatch"`
	LogLevel                  string           `mapstructure:"log_level" json:"log_level"`
}

// ServerConfig contains HTTP server settings
type ServerConfig struct {
	Port            int           `mapstructure:"port" json:"port"`
	PrivatePort     int           `mapstructure:"private_port" json:"private_port"`
	Host            string        `mapstructure:"host" json:"host"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout" json:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout" json:"write_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout" json:"shutdown_timeout"`
}

// DatabaseConfig contains database connection settings
type DatabaseConfig struct {
	Type                  string        `mapstructure:"type" json:"type"`
	Host                  string        `mapstructure:"host" json:"host"`
	Port                  int           `mapstructure:"port" json:"port"`
	Name                  string        `mapstructure:"name" json:"name"`
	Username              string        `mapstructure:"username" json:"username"`
	Password              string        `mapstructure:"password" json:"password"`
	SSLMode               string        `mapstructure:"ssl_mode" json:"ssl_mode"`
	SSLRootCert           string        `mapstructure:"ssl_root_cert" json:"ssl_root_cert"`
	MaxOpenConnections    int           `mapstructure:"max_open_connections" json:"max_open_connections"`
	MaxIdleConnections    int           `mapstructure:"max_idle_connections" json:"max_idle_connections"`
	ConnectionMaxLifetime time.Duration `mapstructure:"connection_max_lifetime" json:"connection_max_lifetime"`
}

// RedisConfig contains Redis connection settings
type RedisConfig struct {
	Enabled  bool   `mapstructure:"enabled" json:"enabled"`
	Host     string `mapstructure:"host" json:"host"`
	Port     int    `mapstructure:"port" json:"port"`
	Password string `mapstructure:"password" json:"password"`
	DB       int    `mapstructure:"db" json:"db"`
	PoolSize int    `mapstructure:"pool_size" json:"pool_size"`
}

// KafkaConfig contains Kafka connection settings
type KafkaConfig struct {
	Enabled          bool          `mapstructure:"enabled" json:"enabled"`
	Brokers          []string      `mapstructure:"brokers" json:"brokers"`
	Topic            string        `mapstructure:"topic" json:"topic"`
	ClientID         string        `mapstructure:"client_id" json:"client_id"`
	Timeout          time.Duration `mapstructure:"timeout" json:"timeout"`
	Retries          int           `mapstructure:"retries" json:"retries"`
	BatchSize        int           `mapstructure:"batch_size" json:"batch_size"`
	CompressionType  string        `mapstructure:"compression_type" json:"compression_type"`
	RequiredAcks     int           `mapstructure:"required_acks" json:"required_acks"`
	SASL             SASLConfig    `mapstructure:"sasl" json:"sasl"`
	TLS              TLSConfig     `mapstructure:"tls" json:"tls"`
	SecurityProtocol string        `mapstructure:"security_protocol" json:"security_protocol"`
}

// SASLConfig contains SASL authentication settings
type SASLConfig struct {
	Enabled   bool   `mapstructure:"enabled" json:"enabled"`
	Mechanism string `mapstructure:"mechanism" json:"mechanism"`
	Username  string `mapstructure:"username" json:"username"`
	Password  string `mapstructure:"password" json:"password"`
}

// TLSConfig contains TLS settings
type TLSConfig struct {
	Enabled            bool   `mapstructure:"enabled" json:"enabled"`
	InsecureSkipVerify bool   `mapstructure:"insecure_skip_verify" json:"insecure_skip_verify"`
	CertFile           string `mapstructure:"cert_file" json:"cert_file"`
	KeyFile            string `mapstructure:"key_file" json:"key_file"`
	CAFile             string `mapstructure:"ca_file" json:"ca_file"`
}

// MetricsConfig contains metrics and monitoring settings
type MetricsConfig struct {
	Port      int    `mapstructure:"port" json:"port"`
	Path      string `mapstructure:"path" json:"path"`
	Enabled   bool   `mapstructure:"enabled" json:"enabled"`
	Namespace string `mapstructure:"namespace" json:"namespace"`
	Subsystem string `mapstructure:"subsystem" json:"subsystem"`
}

// ExportServiceConfig contains export service client settings
type ExportServiceConfig struct {
	BaseURL        string        `mapstructure:"base_url" json:"base_url"`
	PublicBaseURL  string        `mapstructure:"public_base_url" json:"public_base_url"`
	Timeout        time.Duration `mapstructure:"timeout" json:"timeout"`
	MaxRetries     int           `mapstructure:"max_retries" json:"max_retries"`
	PollMaxRetries int           `mapstructure:"poll_max_retries" json:"poll_max_retries"`
	PollInterval   time.Duration `mapstructure:"poll_interval" json:"poll_interval"`
}

// BopConfig contains BOP (Back Office Portal) service settings
type BopConfig struct {
	BaseURL       string `mapstructure:"base_url" json:"base_url"`
	APIToken      string `mapstructure:"api_token" json:"api_token"`
	ClientID      string `mapstructure:"client_id" json:"client_id"`
	InsightsEnv   string `mapstructure:"insights_env" json:"insights_env"`
	Enabled       bool   `mapstructure:"enabled" json:"enabled"`
	EphemeralMode bool   `mapstructure:"ephemeral_mode" json:"ephemeral_mode"`
}

// SchedulerConfig contains scheduler timing and polling settings
type SchedulerConfig struct {
	GracefulShutdownTimeout time.Duration `mapstructure:"graceful_shutdown_timeout" json:"graceful_shutdown_timeout"`
	RedisPollInterval       time.Duration `mapstructure:"redis_poll_interval" json:"redis_poll_interval"`
	DBToRedisSyncInterval   time.Duration `mapstructure:"db_to_redis_sync_interval" json:"db_to_redis_sync_interval"`
	EnablePeriodicSync      bool          `mapstructure:"enable_periodic_sync" json:"enable_periodic_sync"`
	JobRunRetentionCount    int           `mapstructure:"job_run_retention_count" json:"job_run_retention_count"`
	MaxConsecutiveFailures  int           `mapstructure:"max_consecutive_failures" json:"max_consecutive_failures"`
	DenylistJobIDs          []string      `mapstructure:"denylist_job_ids" json:"denylist_job_ids"`
}

// ThreeScaleConfig contains 3scale API Management service settings
type ThreeScaleConfig struct {
	BaseURL string        `mapstructure:"base_url" json:"base_url"`
	Timeout time.Duration `mapstructure:"timeout" json:"timeout"`
	Enabled bool          `mapstructure:"enabled" json:"enabled"`
}

// CloudWatchConfig contains CloudWatch Logs settings
type CloudWatchConfig struct {
	Enabled       bool          `mapstructure:"enabled" json:"enabled"`
	LogGroupName  string        `mapstructure:"log_group_name" json:"log_group_name"`
	LogStreamName string        `mapstructure:"log_stream_name" json:"log_stream_name"`
	Region        string        `mapstructure:"region" json:"region"`
	BufferSize    int           `mapstructure:"buffer_size" json:"buffer_size"`
	FlushInterval time.Duration `mapstructure:"flush_interval" json:"flush_interval"`
	ConsoleOutput bool          `mapstructure:"console_output" json:"console_output"`
}

// LoadConfig loads configuration using Viper with support for config files,
// environment variables, and Clowder integration.
func LoadConfig() (*Config, error) {
	v := viper.New()

	// Config file support (optional)
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("/etc/scheduler")

	// Set all defaults
	setDefaults(v)

	// Bind environment variables
	bindEnvVars(v)

	// Try reading config file (silently ignore if not found)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("error reading config file: %w", err)
			}
		}
	}

	// Unmarshal into config struct
	config := &Config{}
	if err := v.Unmarshal(config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Apply Clowder overrides (highest priority)
	if clowder.IsClowderEnabled() {
		fmt.Println("Clowder enabled!!")
		clowderConfig := clowder.LoadedConfig
		if clowderConfig == nil {
			return nil, fmt.Errorf("failed to load Clowder configuration (nil)")
		}
		applyClowderOverrides(config, clowderConfig)
	}

	// Handle derived/special values (after all sources are applied)
	applyDerivedValues(config)

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// setDefaults registers default values for all configuration keys.
func setDefaults(v *viper.Viper) {
	// Server
	v.SetDefault("server.port", 5000)
	v.SetDefault("server.private_port", 9090)
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.read_timeout", 30*time.Second)
	v.SetDefault("server.write_timeout", 30*time.Second)
	v.SetDefault("server.shutdown_timeout", 10*time.Second)

	// Database
	v.SetDefault("database.type", "postgres")
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.name", "scheduler")
	v.SetDefault("database.username", "insights")
	v.SetDefault("database.password", "insights")
	v.SetDefault("database.ssl_mode", "disable")
	v.SetDefault("database.ssl_root_cert", "")
	v.SetDefault("database.max_open_connections", 25)
	v.SetDefault("database.max_idle_connections", 5)
	v.SetDefault("database.connection_max_lifetime", 5*time.Minute)

	// Redis
	v.SetDefault("redis.enabled", false)
	v.SetDefault("redis.host", "localhost")
	v.SetDefault("redis.port", 6379)
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("redis.pool_size", 10)

	// Kafka
	v.SetDefault("kafka.enabled", false)
	v.SetDefault("kafka.brokers", []string{})
	v.SetDefault("kafka.topic", "platform.notifications.ingress")
	v.SetDefault("kafka.security_protocol", "")
	v.SetDefault("kafka.client_id", "insights-scheduler")
	v.SetDefault("kafka.timeout", 30*time.Second)
	v.SetDefault("kafka.retries", 5)
	v.SetDefault("kafka.batch_size", 100)
	v.SetDefault("kafka.compression_type", "snappy")
	v.SetDefault("kafka.required_acks", -1)
	v.SetDefault("kafka.sasl.enabled", false)
	v.SetDefault("kafka.sasl.mechanism", "PLAIN")
	v.SetDefault("kafka.sasl.username", "")
	v.SetDefault("kafka.sasl.password", "")
	v.SetDefault("kafka.tls.enabled", false)
	v.SetDefault("kafka.tls.insecure_skip_verify", false)
	v.SetDefault("kafka.tls.cert_file", "")
	v.SetDefault("kafka.tls.key_file", "")
	v.SetDefault("kafka.tls.ca_file", "")

	// Metrics
	v.SetDefault("metrics.port", 8080)
	v.SetDefault("metrics.path", "/metrics")
	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.namespace", "insights")
	v.SetDefault("metrics.subsystem", "scheduler")

	// Export Service
	v.SetDefault("export_service.base_url", "http://export-service-service:8000/api/export/v1")
	v.SetDefault("export_service.public_base_url", "")
	v.SetDefault("export_service.timeout", 5*time.Minute)
	v.SetDefault("export_service.max_retries", 3)
	v.SetDefault("export_service.poll_max_retries", 60)
	v.SetDefault("export_service.poll_interval", 5*time.Second)

	// BOP
	v.SetDefault("bop.base_url", "https://backoffice.apps.ext.spoke.preprod.us-east-1.aws.paas.redhat.com")
	v.SetDefault("bop.api_token", "")
	v.SetDefault("bop.client_id", "insights-scheduler")
	v.SetDefault("bop.insights_env", "preprod")
	v.SetDefault("bop.enabled", true)
	v.SetDefault("bop.ephemeral_mode", false)

	// Scheduler
	v.SetDefault("scheduler.graceful_shutdown_timeout", 30*time.Second)
	v.SetDefault("scheduler.redis_poll_interval", 10*time.Second)
	v.SetDefault("scheduler.db_to_redis_sync_interval", 1*time.Hour)
	v.SetDefault("scheduler.enable_periodic_sync", false)
	v.SetDefault("scheduler.job_run_retention_count", 10)
	v.SetDefault("scheduler.max_consecutive_failures", 3)
	v.SetDefault("scheduler.denylist_job_ids", []string{})

	// 3scale
	v.SetDefault("threescale.base_url", "http://3scale-service:8000")
	v.SetDefault("threescale.timeout", 5*time.Second)
	v.SetDefault("threescale.enabled", true)

	// CloudWatch
	v.SetDefault("cloudwatch.enabled", false)
	v.SetDefault("cloudwatch.log_group_name", "/aws/insights-scheduler")
	v.SetDefault("cloudwatch.log_stream_name", "default")
	v.SetDefault("cloudwatch.region", "us-east-1")
	v.SetDefault("cloudwatch.buffer_size", 100)
	v.SetDefault("cloudwatch.flush_interval", 5*time.Second)
	v.SetDefault("cloudwatch.console_output", true)

	// Logging
	v.SetDefault("log_level", "info")

	// Feature flags
	v.SetDefault("user_validator_impl", "bop")
	v.SetDefault("job_completion_notifier_impl", "notifications")
}

// bindEnvVars maps environment variable names to viper configuration keys.
// This preserves backward compatibility with all existing env var names.
func bindEnvVars(v *viper.Viper) {
	// Server
	_ = v.BindEnv("server.port", "PORT")
	_ = v.BindEnv("server.private_port", "PRIVATE_PORT")
	_ = v.BindEnv("server.host", "HOST")
	_ = v.BindEnv("server.read_timeout", "READ_TIMEOUT")
	_ = v.BindEnv("server.write_timeout", "WRITE_TIMEOUT")
	_ = v.BindEnv("server.shutdown_timeout", "SHUTDOWN_TIMEOUT")

	// Database
	_ = v.BindEnv("database.type", "DB_TYPE")
	_ = v.BindEnv("database.host", "DB_HOST")
	_ = v.BindEnv("database.port", "DB_PORT")
	_ = v.BindEnv("database.name", "DB_NAME")
	_ = v.BindEnv("database.username", "DB_USERNAME")
	_ = v.BindEnv("database.password", "DB_PASSWORD")
	_ = v.BindEnv("database.ssl_mode", "DB_SSL_MODE")
	_ = v.BindEnv("database.max_open_connections", "DB_MAX_OPEN_CONNECTIONS")
	_ = v.BindEnv("database.max_idle_connections", "DB_MAX_IDLE_CONNECTIONS")
	_ = v.BindEnv("database.connection_max_lifetime", "DB_CONNECTION_MAX_LIFETIME")

	// Redis
	_ = v.BindEnv("redis.enabled", "REDIS_ENABLED")
	_ = v.BindEnv("redis.host", "REDIS_HOST")
	_ = v.BindEnv("redis.port", "REDIS_PORT")
	_ = v.BindEnv("redis.password", "REDIS_PASSWORD")
	_ = v.BindEnv("redis.db", "REDIS_DB")
	_ = v.BindEnv("redis.pool_size", "REDIS_POOL_SIZE")

	// Kafka
	_ = v.BindEnv("kafka.brokers", "KAFKA_BROKERS")
	_ = v.BindEnv("kafka.topic", "KAFKA_TOPIC")
	_ = v.BindEnv("kafka.security_protocol", "KAFKA_SECURITY_PROTOCOL")
	_ = v.BindEnv("kafka.client_id", "KAFKA_CLIENT_ID")
	_ = v.BindEnv("kafka.timeout", "KAFKA_TIMEOUT")
	_ = v.BindEnv("kafka.retries", "KAFKA_RETRIES")
	_ = v.BindEnv("kafka.batch_size", "KAFKA_BATCH_SIZE")
	_ = v.BindEnv("kafka.compression_type", "KAFKA_COMPRESSION")
	_ = v.BindEnv("kafka.required_acks", "KAFKA_REQUIRED_ACKS")
	_ = v.BindEnv("kafka.sasl.enabled", "KAFKA_SASL_ENABLED")
	_ = v.BindEnv("kafka.sasl.mechanism", "KAFKA_SASL_MECHANISM")
	_ = v.BindEnv("kafka.sasl.username", "KAFKA_SASL_USERNAME")
	_ = v.BindEnv("kafka.sasl.password", "KAFKA_SASL_PASSWORD")
	_ = v.BindEnv("kafka.tls.enabled", "KAFKA_TLS_ENABLED")
	_ = v.BindEnv("kafka.tls.insecure_skip_verify", "KAFKA_TLS_INSECURE_SKIP_VERIFY")
	_ = v.BindEnv("kafka.tls.cert_file", "KAFKA_TLS_CERT_FILE")
	_ = v.BindEnv("kafka.tls.key_file", "KAFKA_TLS_KEY_FILE")
	_ = v.BindEnv("kafka.tls.ca_file", "KAFKA_TLS_CA_FILE")

	// Metrics
	_ = v.BindEnv("metrics.port", "METRICS_PORT")
	_ = v.BindEnv("metrics.path", "METRICS_PATH")
	_ = v.BindEnv("metrics.enabled", "METRICS_ENABLED")
	_ = v.BindEnv("metrics.namespace", "METRICS_NAMESPACE")
	_ = v.BindEnv("metrics.subsystem", "METRICS_SUBSYSTEM")

	// Export Service
	_ = v.BindEnv("export_service.base_url", "EXPORT_SERVICE_URL")
	_ = v.BindEnv("export_service.public_base_url", "EXPORT_SERVICE_PUBLIC_URL")
	_ = v.BindEnv("export_service.timeout", "EXPORT_SERVICE_TIMEOUT")
	_ = v.BindEnv("export_service.max_retries", "EXPORT_SERVICE_MAX_RETRIES")
	_ = v.BindEnv("export_service.poll_max_retries", "EXPORT_SERVICE_POLL_MAX_RETRIES")
	_ = v.BindEnv("export_service.poll_interval", "EXPORT_SERVICE_POLL_INTERVAL")

	// BOP
	_ = v.BindEnv("bop.base_url", "BOP_URL")
	_ = v.BindEnv("bop.api_token", "BOP_API_TOKEN")
	_ = v.BindEnv("bop.client_id", "BOP_CLIENT_ID")
	_ = v.BindEnv("bop.insights_env", "BOP_INSIGHTS_ENV")
	_ = v.BindEnv("bop.enabled", "BOP_ENABLED")
	_ = v.BindEnv("bop.ephemeral_mode", "BOP_EPHEMERAL_MODE")

	// Scheduler
	_ = v.BindEnv("scheduler.graceful_shutdown_timeout", "SCHEDULER_GRACEFUL_SHUTDOWN_TIMEOUT")
	_ = v.BindEnv("scheduler.redis_poll_interval", "SCHEDULER_REDIS_POLL_INTERVAL")
	_ = v.BindEnv("scheduler.db_to_redis_sync_interval", "SCHEDULER_DB_TO_REDIS_SYNC_INTERVAL")
	_ = v.BindEnv("scheduler.enable_periodic_sync", "ENABLE_PERIODIC_SYNC")
	_ = v.BindEnv("scheduler.job_run_retention_count", "JOB_RUN_RETENTION_COUNT")
	_ = v.BindEnv("scheduler.max_consecutive_failures", "MAX_CONSECUTIVE_FAILURES")
	_ = v.BindEnv("scheduler.denylist_job_ids", "SCHEDULER_DENYLIST_JOB_IDS")

	// 3scale
	_ = v.BindEnv("threescale.base_url", "THREESCALE_URL")
	_ = v.BindEnv("threescale.timeout", "THREESCALE_TIMEOUT")
	_ = v.BindEnv("threescale.enabled", "THREESCALE_ENABLED")

	// CloudWatch
	_ = v.BindEnv("cloudwatch.enabled", "CLOUDWATCH_ENABLED")
	_ = v.BindEnv("cloudwatch.log_group_name", "CLOUDWATCH_LOG_GROUP_NAME")
	_ = v.BindEnv("cloudwatch.log_stream_name", "CLOUDWATCH_LOG_STREAM_NAME")
	_ = v.BindEnv("cloudwatch.region", "CLOUDWATCH_REGION")
	_ = v.BindEnv("cloudwatch.buffer_size", "CLOUDWATCH_BUFFER_SIZE")
	_ = v.BindEnv("cloudwatch.flush_interval", "CLOUDWATCH_FLUSH_INTERVAL")
	_ = v.BindEnv("cloudwatch.console_output", "CLOUDWATCH_CONSOLE_OUTPUT")

	// Logging
	_ = v.BindEnv("log_level", "LOG_LEVEL")

	// Feature flags
	_ = v.BindEnv("user_validator_impl", "USER_VALIDATOR_IMPL")
	_ = v.BindEnv("job_completion_notifier_impl", "JOB_COMPLETION_NOTIFIER_IMPL")
}

// applyClowderOverrides applies Clowder platform configuration on top of
// viper-loaded values. Clowder is the highest-priority config source in
// production Red Hat deployments.
func applyClowderOverrides(config *Config, clowderConfig *clowder.AppConfig) {
	// Server
	if clowderConfig.PublicPort != nil {
		config.Server.Port = *clowderConfig.PublicPort
	}
	if clowderConfig.PrivatePort != nil {
		config.Server.PrivatePort = *clowderConfig.PrivatePort
	}

	// Database
	if clowderConfig.Database != nil {
		config.Database.Type = "postgres"
		config.Database.Host = clowderConfig.Database.Hostname
		config.Database.Port = clowderConfig.Database.Port
		config.Database.Name = clowderConfig.Database.Name
		config.Database.Username = clowderConfig.Database.Username
		config.Database.Password = clowderConfig.Database.Password
		config.Database.SSLMode = clowderConfig.Database.SslMode

		if clowderConfig.Database.RdsCa != nil {
			pathToDBCertFile, err := clowderConfig.RdsCa()
			if err != nil {
				panic(err)
			}
			config.Database.SSLRootCert = pathToDBCertFile
		}
	}

	// Redis
	if clowderConfig.InMemoryDb != nil {
		config.Redis.Enabled = true
		config.Redis.Host = clowderConfig.InMemoryDb.Hostname
		config.Redis.Port = clowderConfig.InMemoryDb.Port

		if clowderConfig.InMemoryDb.Password != nil && *clowderConfig.InMemoryDb.Password != "" {
			config.Redis.Password = *clowderConfig.InMemoryDb.Password
			fmt.Println("Redis authentication: password configured")
		}

		if clowderConfig.InMemoryDb.Username != nil && *clowderConfig.InMemoryDb.Username != "" {
			fmt.Printf("Redis username provided: %s (note: currently only password auth is used)\n", *clowderConfig.InMemoryDb.Username)
		}
	} else {
		if config.Redis.Enabled {
			fmt.Printf("Redis: %s:%d\n", config.Redis.Host, config.Redis.Port)
		} else {
			fmt.Println("Redis: disabled")
		}
	}

	// Kafka
	if clowderConfig.Kafka != nil {
		config.Kafka.Enabled = true
		config.Kafka.Brokers = []string{}

		for _, broker := range clowderConfig.Kafka.Brokers {
			config.Kafka.Brokers = append(config.Kafka.Brokers, fmt.Sprintf("%s:%d", broker.Hostname, *broker.Port))
		}

		for _, topicConfig := range clowderConfig.Kafka.Topics {
			if topicConfig.RequestedName == config.Kafka.Topic || topicConfig.Name == config.Kafka.Topic {
				config.Kafka.Topic = topicConfig.Name
				break
			}
		}

		if len(clowderConfig.Kafka.Brokers) > 0 && clowderConfig.Kafka.Brokers[0].Sasl != nil {
			config.Kafka.SASL.Enabled = true
			if clowderConfig.Kafka.Brokers[0].Sasl.SaslMechanism != nil {
				config.Kafka.SASL.Mechanism = *clowderConfig.Kafka.Brokers[0].Sasl.SaslMechanism
			}
			if clowderConfig.Kafka.Brokers[0].Sasl.Username != nil {
				config.Kafka.SASL.Username = *clowderConfig.Kafka.Brokers[0].Sasl.Username
			}
			if clowderConfig.Kafka.Brokers[0].Sasl.Password != nil {
				config.Kafka.SASL.Password = *clowderConfig.Kafka.Brokers[0].Sasl.Password
			}
		}

		if clowderConfig.Kafka.Brokers[0].Cacert != nil {
			fmt.Println("Kafka is using ssl/tls")
			caPath, err := clowderConfig.KafkaCa(clowderConfig.Kafka.Brokers[0])
			if err != nil {
				panic("Kafka CA cert failed to write")
			}
			config.Kafka.TLS.Enabled = true
			config.Kafka.TLS.CAFile = caPath
		}

		if clowderConfig.Kafka.Brokers[0].SecurityProtocol != nil && *clowderConfig.Kafka.Brokers[0].SecurityProtocol != "" {
			config.Kafka.SecurityProtocol = *clowderConfig.Kafka.Brokers[0].SecurityProtocol
		} else if clowderConfig.Kafka.Brokers[0].Sasl != nil && clowderConfig.Kafka.Brokers[0].Sasl.SecurityProtocol != nil && *clowderConfig.Kafka.Brokers[0].Sasl.SecurityProtocol != "" {
			config.Kafka.SecurityProtocol = *clowderConfig.Kafka.Brokers[0].Sasl.SecurityProtocol
		}
	}

	// Metrics
	config.Metrics.Port = clowderConfig.MetricsPort
	config.Metrics.Path = clowderConfig.MetricsPath

	// CloudWatch Logging
	// Clowder provides CloudWatch configuration through the Logging section
	if clowderConfig.Logging.Cloudwatch != nil && clowderConfig.Logging.Cloudwatch.AccessKeyId != "" {
		config.CloudWatch.Enabled = true
		config.CloudWatch.Region = clowderConfig.Logging.Cloudwatch.Region
		config.CloudWatch.LogGroupName = clowderConfig.Logging.Cloudwatch.LogGroup

		// Use hostname or pod name for log stream name if available
		if hostname, err := os.Hostname(); err == nil {
			config.CloudWatch.LogStreamName = hostname
		}

		// Access key can be provided via Clowder or fall back to environment/IAM
		if clowderConfig.Logging.Cloudwatch.AccessKeyId != "" {
			os.Setenv("AWS_ACCESS_KEY_ID", clowderConfig.Logging.Cloudwatch.AccessKeyId)
		}
		if clowderConfig.Logging.Cloudwatch.SecretAccessKey != "" {
			os.Setenv("AWS_SECRET_ACCESS_KEY", clowderConfig.Logging.Cloudwatch.SecretAccessKey)
		}

		fmt.Printf("CloudWatch logging enabled via Clowder: group=%s, stream=%s, region=%s\n",
			config.CloudWatch.LogGroupName, config.CloudWatch.LogStreamName, config.CloudWatch.Region)
	}
}

// applyDerivedValues computes values that depend on other config fields.
// Called after all config sources (viper, Clowder) have been applied.
func applyDerivedValues(config *Config) {
	// Kafka enabled is derived from having brokers configured
	if len(config.Kafka.Brokers) > 0 {
		config.Kafka.Enabled = true
	}

	// BOP enabled requires an API token
	if config.Bop.APIToken == "" {
		config.Bop.Enabled = false
	}

	// Export service public URL defaults to base URL
	if config.ExportService.PublicBaseURL == "" {
		config.ExportService.PublicBaseURL = config.ExportService.BaseURL
	}

	// BOP ephemeral mode dynamically constructs the BOP URL from the K8s namespace
	if config.Bop.EphemeralMode {
		namespace, err := getOpenshiftNamespace()
		if err != nil {
			panic("Running in BOP ephemeral mode but cannot determine OpenShift namespace")
		}
		config.Bop.BaseURL = fmt.Sprintf("http://env-%s-mbop.%s.svc.cluster.local:8090", namespace, namespace)
	}
}

func getOpenshiftNamespace() (string, error) {
	filePath := "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %v", err)
	}

	return string(contentBytes), nil
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
	if c.Database.Type != "postgres" && c.Database.Type != "postgresql" {
		return fmt.Errorf("unsupported database type: %s (only 'postgres' is supported)", c.Database.Type)
	}
	if c.Database.Host == "" {
		return fmt.Errorf("database host is required")
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

	// Validate Redis configuration
	if c.Redis.Enabled {
		if c.Redis.Host == "" {
			return fmt.Errorf("redis host is required when redis is enabled")
		}
		if c.Redis.Port < 1 || c.Redis.Port > 65535 {
			return fmt.Errorf("invalid redis port: %d", c.Redis.Port)
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

	// Validate 3scale configuration (only if enabled)
	if c.ThreeScale.Enabled {
		if c.ThreeScale.BaseURL == "" {
			return fmt.Errorf("3scale base URL is required when 3scale is enabled")
		}
	}

	return nil
}
