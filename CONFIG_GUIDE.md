# Configuration Guide

The Insights Scheduler uses a centralized configuration system that integrates with Red Hat's Clowder configuration management via app-common-go, with fallback to environment variables and sensible defaults.

## Configuration Structure

The application configuration is organized into several categories:

### Server Configuration

Controls HTTP server behavior:

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `HOST` | Server bind address | `0.0.0.0` | `localhost` |
| `PORT` | Main HTTP server port | `5000` | `8080` |
| `PRIVATE_PORT` | Internal/admin endpoints port | `9090` | `9999` |
| `READ_TIMEOUT` | HTTP request timeout | `30s` | `1m` |
| `WRITE_TIMEOUT` | HTTP response timeout | `30s` | `1m` |
| `SHUTDOWN_TIMEOUT` | Graceful shutdown timeout | `10s` | `30s` |

### Database Configuration

Controls database connectivity:

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `DB_TYPE` | Database type | `sqlite` | `postgres`, `mysql` |
| `DB_PATH` | SQLite database file path | `./jobs.db` | `/data/jobs.db` |
| `DB_HOST` | External database host | `localhost` | `postgres.example.com` |
| `DB_PORT` | External database port | `5432` | `3306` |
| `DB_NAME` | Database name | `insights_scheduler` | `production_db` |
| `DB_USERNAME` | Database username | `""` | `scheduler_user` |
| `DB_PASSWORD` | Database password | `""` | `secret123` |
| `DB_SSL_MODE` | SSL mode for PostgreSQL | `disable` | `require`, `verify-full` |
| `DB_MAX_OPEN_CONNECTIONS` | Max open connections | `25` | `50` |
| `DB_MAX_IDLE_CONNECTIONS` | Max idle connections | `5` | `10` |
| `DB_CONNECTION_MAX_LIFETIME` | Connection max lifetime | `5m` | `1h` |

### Kafka Configuration

Controls Kafka message publishing:

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `KAFKA_BROKERS` | Comma-separated broker list | `""` (disabled) | `broker1:9092,broker2:9092` |
| `KAFKA_TOPIC` | Topic for export messages | `platform.notifications.ingress` | `insights-exports` |
| `KAFKA_CLIENT_ID` | Producer client ID | `insights-scheduler` | `scheduler-prod` |
| `KAFKA_TIMEOUT` | Operation timeout | `30s` | `1m` |
| `KAFKA_RETRIES` | Retry attempts | `5` | `10` |
| `KAFKA_COMPRESSION` | Compression type | `snappy` | `gzip`, `lz4`, `zstd` |
| `KAFKA_REQUIRED_ACKS` | Acknowledgment level | `-1` (all replicas) | `0`, `1` |

#### SASL Authentication (Optional)

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `KAFKA_SASL_ENABLED` | Enable SASL auth | `false` | `true` |
| `KAFKA_SASL_MECHANISM` | SASL mechanism | `PLAIN` | `SCRAM-SHA-256` |
| `KAFKA_SASL_USERNAME` | SASL username | `""` | `kafka_user` |
| `KAFKA_SASL_PASSWORD` | SASL password | `""` | `kafka_pass` |

#### TLS Configuration (Optional)

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `KAFKA_TLS_ENABLED` | Enable TLS | `false` | `true` |
| `KAFKA_TLS_INSECURE_SKIP_VERIFY` | Skip cert validation | `false` | `true` |
| `KAFKA_TLS_CERT_FILE` | Client certificate path | `""` | `/certs/client.crt` |
| `KAFKA_TLS_KEY_FILE` | Client key path | `""` | `/certs/client.key` |
| `KAFKA_TLS_CA_FILE` | CA certificate path | `""` | `/certs/ca.crt` |

### Metrics Configuration

Controls metrics and monitoring:

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `METRICS_PORT` | Metrics endpoint port | `8080` | `9090` |
| `METRICS_PATH` | Metrics endpoint path | `/metrics` | `/prometheus` |
| `METRICS_ENABLED` | Enable metrics | `true` | `false` |
| `METRICS_NAMESPACE` | Metric name prefix | `insights` | `myapp` |
| `METRICS_SUBSYSTEM` | Metric subsystem | `scheduler` | `jobs` |

### Export Service Configuration

Controls Red Hat Export Service integration:

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `EXPORT_SERVICE_URL` | Export service base URL | `http://localhost:9000/api/export/v1` | `https://api.redhat.com/export/v1` |
| `EXPORT_SERVICE_ACCOUNT` | Account number | `000002` | `123456` |
| `EXPORT_SERVICE_ORG_ID` | Organization ID | `000001` | `org-789` |
| `EXPORT_SERVICE_TIMEOUT` | Request timeout | `5m` | `10m` |
| `EXPORT_SERVICE_MAX_RETRIES` | Max retry attempts | `3` | `5` |

## Configuration Examples

### Development Environment

```bash
# Minimal configuration for local development
export PORT=8000
export DB_PATH="./dev_jobs.db"
export METRICS_PORT=9090
# Kafka disabled (KAFKA_BROKERS not set)
```

### Production Environment

```bash
# Production configuration
export HOST="0.0.0.0"
export PORT="5000"
export PRIVATE_PORT="9090"
export SHUTDOWN_TIMEOUT="30s"

# Database
export DB_TYPE="postgres"
export DB_HOST="postgres.prod.com"
export DB_PORT="5432"
export DB_NAME="insights_scheduler"
export DB_USERNAME="scheduler"
export DB_PASSWORD="$(cat /var/secrets/db-password)"
export DB_SSL_MODE="require"
export DB_MAX_OPEN_CONNECTIONS="50"

# Kafka with TLS
export KAFKA_BROKERS="kafka1.prod.com:9092,kafka2.prod.com:9092,kafka3.prod.com:9092"
export KAFKA_TOPIC="platform.notifications.ingress"
export KAFKA_TLS_ENABLED="true"
export KAFKA_TLS_CERT_FILE="/etc/kafka/certs/client.crt"
export KAFKA_TLS_KEY_FILE="/etc/kafka/certs/client.key"
export KAFKA_TLS_CA_FILE="/etc/kafka/certs/ca.crt"

# Metrics
export METRICS_ENABLED="true"
export METRICS_PORT="8080"

# Export service
export EXPORT_SERVICE_URL="https://console.redhat.com/api/export/v1"
export EXPORT_SERVICE_ACCOUNT="$(cat /var/secrets/account-number)"
export EXPORT_SERVICE_ORG_ID="$(cat /var/secrets/org-id)"
```

### OpenShift Environment (with Clowder)

When deployed in Red Hat OpenShift with Clowder, most configuration is automatically managed:

```yaml
# ClowdApp resource for Clowder integration
apiVersion: cloud.redhat.com/v1alpha1
kind: ClowdApp
metadata:
  name: insights-scheduler
spec:
  envName: myenv
  database:
    name: scheduler
  kafka:
    topics:
    - topicName: platform.notifications.ingress
      partitions: 3
  web: true
  metrics:
    port: 8080
    path: /metrics
```

Additional environment-specific overrides:
```bash
# Only needed for non-Clowder settings
oc create secret generic insights-scheduler-secrets \
  --from-literal=EXPORT_SERVICE_ACCOUNT="123456" \
  --from-literal=EXPORT_SERVICE_ORG_ID="org-789"
```

### OpenShift Environment (without Clowder)

```bash
# Set via ConfigMap and Secrets in OpenShift
oc create configmap insights-scheduler-config \
  --from-literal=PORT=5000 \
  --from-literal=KAFKA_BROKERS="kafka1:9092,kafka2:9092" \
  --from-literal=KAFKA_TOPIC="platform.notifications.ingress" \
  --from-literal=METRICS_ENABLED="true"

oc create secret generic insights-scheduler-secrets \
  --from-literal=EXPORT_SERVICE_ACCOUNT="123456" \
  --from-literal=EXPORT_SERVICE_ORG_ID="org-789"
```

## Configuration Validation

The application validates configuration on startup and will fail with descriptive errors for invalid settings:

- **Port ranges**: All ports must be 1-65535
- **Database requirements**: SQLite needs path, external DBs need host
- **Kafka validation**: If enabled, brokers and topic are required
- **Export service**: URL, account, and org ID are mandatory

## Configuration Loading

Configuration is loaded in this order (later values override earlier ones):

1. **Default values** - Built-in sensible defaults
2. **Environment variables** - Override defaults  
3. **Clowder configuration** - Red Hat's app-common-go integration (when available)
4. **Validation** - Ensures configuration is valid

### Clowder Integration

When running in a Red Hat OpenShift environment with Clowder enabled, the application automatically detects and uses Clowder configuration for:

- **Server ports**: Uses `PublicPort` and `PrivatePort` from Clowder
- **Database**: Automatically configures PostgreSQL connection from Clowder database config
- **Kafka**: Uses broker addresses, topics, and SASL authentication from Clowder
- **Metrics**: Uses `MetricsPort` and `MetricsPath` from Clowder

Environment variables still work as overrides for settings not provided by Clowder.

## Runtime Configuration Changes

Most configuration requires application restart. For runtime changes:

- **Kafka**: Restart required for broker/topic changes
- **Database**: Restart required for connection changes  
- **Server ports**: Restart required
- **Metrics**: Can be toggled via environment variable + restart

## Troubleshooting

### Common Configuration Issues

1. **Invalid port numbers**: Check port ranges (1-65535)
2. **Database connection**: Verify host, port, credentials
3. **Kafka connection**: Check broker addresses and network connectivity
4. **Missing secrets**: Ensure EXPORT_SERVICE_ACCOUNT and EXPORT_SERVICE_ORG_ID are set

### Configuration Debugging

Enable debug logging to see loaded configuration:

```bash
export LOG_LEVEL=DEBUG
./scheduler
```

Look for log messages:
- `"Starting Insights Scheduler with configuration:"`
- `"Database initialized successfully"`
- `"Kafka producer initialized successfully"` or `"Kafka disabled"`
- `"Clowder configuration loaded successfully"` (when using Clowder)
- `"Using environment variable configuration"` (when Clowder not available)

### Validation Errors

Common validation error messages:

- `"invalid server port: X"` - Port outside valid range
- `"database type is required"` - DB_TYPE not set or empty
- `"kafka brokers are required when kafka is enabled"` - KAFKA_BROKERS empty but Kafka expected
- `"export service base URL is required"` - EXPORT_SERVICE_URL missing

## Best Practices

1. **Use Clowder when available** in Red Hat OpenShift environments for standardized configuration
2. **Use environment variables** for configuration when Clowder is not available
3. **Store secrets securely** (not in plain text files) - use OpenShift Secrets or external secret management
4. **Validate configuration** in staging before production
5. **Monitor metrics** to ensure proper operation
6. **Use structured logging** to debug configuration issues
7. **Document environment-specific** overrides
8. **Test configuration changes** in non-production first
9. **Leverage ClowdApp resources** for infrastructure configuration in Red Hat environments
10. **Ensure graceful degradation** when optional services (like Kafka) are unavailable