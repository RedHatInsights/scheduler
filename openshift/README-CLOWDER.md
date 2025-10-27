# Clowder Deployment Guide

This directory contains Clowder-based deployment configurations for the Insights Scheduler service.

## Overview

The ClowdApp configuration provides Red Hat's standardized way to deploy applications in OpenShift environments with automatic infrastructure provisioning including:

- PostgreSQL database
- Kafka topics and configuration
- Service discovery
- Metrics and monitoring
- Auto-scaling

## Files Structure

```
openshift/
├── clowdapp.yaml              # Main ClowdApp resource
├── kustomization.yaml         # Base kustomization
├── overlays/
│   ├── stage/
│   │   ├── kustomization.yaml
│   │   └── stage-patches.yaml
│   └── prod/
│       ├── kustomization.yaml
│       └── prod-patches.yaml
└── README-CLOWDER.md
```

## ClowdApp Features

### Database Configuration
- **Type**: PostgreSQL (managed by Clowder)
- **Version**: PostgreSQL 13
- **Name**: `scheduler`
- **Automatic**: Connection details provided via environment variables

### Kafka Configuration
- **Topic**: `platform.notifications.ingress`
- **Partitions**: 3 (stage), 6 (prod)
- **Replicas**: 3
- **Retention**: 7 days (stage), 30 days (prod)
- **Automatic**: Broker addresses and authentication provided by Clowder

### Monitoring
- **Metrics Port**: 8080
- **Metrics Path**: `/metrics`
- **ServiceMonitor**: Automatically configured for Prometheus scraping
- **Auto-scaling**: HPA based on CPU/memory utilization

### Security
- **Non-root user**: Runs as user 1001
- **Security context**: Restricted capabilities
- **Secrets**: Managed via Kubernetes secrets

## Deployment Instructions

### Prerequisites

1. **Clowder Environment**: Ensure you have access to a Clowder-enabled OpenShift cluster
2. **Container Image**: Build and push the container image to a registry
3. **Secrets**: Prepare environment-specific secrets

### Stage Deployment

```bash
# Set environment variables
export EXPORT_SERVICE_ACCOUNT="stage-account-123"
export EXPORT_SERVICE_ORG_ID="stage-org-456"

# Deploy to stage
oc apply -k openshift/overlays/stage/
```

### Production Deployment

```bash
# Set environment variables (use actual production values)
export EXPORT_SERVICE_ACCOUNT_PROD="prod-account-789"
export EXPORT_SERVICE_ORG_ID_PROD="prod-org-012"

# Deploy to production
oc apply -k openshift/overlays/prod/
```

### Manual Deployment (Base)

```bash
# Set required environment variables
export ENV_NAME="your-clowder-env"
export IMAGE_TAG="quay.io/your-org/insights-scheduler:latest"
export EXPORT_SERVICE_ACCOUNT="your-account"
export EXPORT_SERVICE_ORG_ID="your-org-id"

# Apply the base configuration
envsubst < openshift/clowdapp.yaml | oc apply -f -
```

## Configuration

### Environment Variables

The ClowdApp automatically provides these variables via Clowder:

#### Database (Automatic)
- `DATABASE_HOST`
- `DATABASE_PORT`
- `DATABASE_NAME`
- `DATABASE_USER`
- `DATABASE_PASSWORD`
- `DATABASE_SSLMODE`

#### Kafka (Automatic)
- `KAFKA_BROKERS` (comma-separated list)
- `KAFKA_TOPICS` (JSON configuration)
- `KAFKA_SASL_USERNAME`
- `KAFKA_SASL_PASSWORD`
- `KAFKA_SASL_MECHANISM`

#### Service Ports (Automatic)
- `PORT` (public port)
- `PRIVATE_PORT` (private/admin port)
- `METRICS_PORT`

#### Application-Specific (Manual)
- `EXPORT_SERVICE_ACCOUNT`
- `EXPORT_SERVICE_ORG_ID`
- `LOG_LEVEL`
- `EXPORT_SERVICE_TIMEOUT`
- `KAFKA_CLIENT_ID`

### Customization

#### Environment-Specific Overrides

Create overlays for different environments:

```yaml
# overlays/dev/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - ../../

patches:
  - target:
      kind: ClowdApp
      name: insights-scheduler
    patch: |-
      - op: replace
        path: /spec/envName
        value: insights-dev
```

#### Resource Scaling

Modify replica counts and resource limits:

```yaml
patches:
  - target:
      kind: ClowdApp
      name: insights-scheduler
    patch: |-
      - op: replace
        path: /spec/deployments/0/minReplicas
        value: 1
      - op: replace
        path: /spec/deployments/0/maxReplicas
        value: 5
```

## Monitoring and Observability

### Metrics
- **Endpoint**: `http://service:8080/metrics`
- **Format**: Prometheus format
- **Auto-discovery**: ServiceMonitor configured

### Health Checks
- **Liveness**: `/health` endpoint
- **Readiness**: `/ready` endpoint
- **Startup**: `/health` endpoint with extended timeout

### Logging
- **Format**: Structured JSON logs
- **Level**: Configurable via `LOG_LEVEL` environment variable
- **Aggregation**: Automatic via OpenShift logging

## Troubleshooting

### Common Issues

1. **ClowdApp not found**
   ```bash
   # Check if Clowder operator is installed
   oc get crd clowdapps.cloud.redhat.com
   ```

2. **Database connection failures**
   ```bash
   # Check database credentials
   oc get secret insights-scheduler-db -o yaml
   ```

3. **Kafka connection issues**
   ```bash
   # Check Kafka configuration
   oc get secret insights-scheduler-kafka -o yaml
   ```

4. **Image pull errors**
   ```bash
   # Check image registry access
   oc describe pod <pod-name>
   ```

### Debugging Commands

```bash
# Check ClowdApp status
oc get clowdapp insights-scheduler -o yaml

# View generated resources
oc get all -l app=insights-scheduler

# Check logs
oc logs deployment/insights-scheduler-service

# Port forward for local testing
oc port-forward svc/insights-scheduler 8080:8080
```

## Best Practices

1. **Use Kustomize overlays** for environment-specific configurations
2. **Store secrets externally** (Vault, sealed-secrets, etc.)
3. **Monitor resource usage** and adjust limits accordingly
4. **Test in stage** before deploying to production
5. **Use specific image tags** rather than `latest`
6. **Enable auto-scaling** for production workloads
7. **Configure proper health checks** for reliable deployments

## Migration from Standard Deployment

If migrating from the standard OpenShift deployment:

1. **Database**: Migrate data from SQLite to PostgreSQL
2. **Configuration**: Update to use Clowder-provided environment variables
3. **Secrets**: Move to Kubernetes secrets management
4. **Monitoring**: Update to use ServiceMonitor instead of manual configuration

## Additional Resources

- [Clowder Documentation](https://github.com/RedHatInsights/clowder)
- [ClowdApp API Reference](https://redhatinsights.github.io/clowder/api/)
- [Kustomize Documentation](https://kustomize.io/)
- [OpenShift Container Platform Documentation](https://docs.openshift.com/)