# OpenShift Deployment Guide

This directory contains OpenShift deployment configurations for the Insights Scheduler application.

## Prerequisites

1. **OpenShift CLI (oc)** - [Installation Guide](https://docs.openshift.com/container-platform/latest/cli_reference/openshift_cli/getting-started-cli.html)
2. **Container tool** - Either Podman or Docker
3. **Access to an image registry** - Quay.io, Docker Hub, or internal registry
4. **OpenShift cluster access** with sufficient permissions

## Quick Deployment

1. **Login to OpenShift:**
   ```bash
   oc login https://your-openshift-cluster.com
   ```

2. **Set environment variables:**
   ```bash
   export IMAGE_REGISTRY="quay.io"
   export IMAGE_ORG="your-organization"
   export IMAGE_TAG="v1.0.0"
   ```

3. **Run the deployment script:**
   ```bash
   ./openshift/build-and-deploy.sh
   ```

## Manual Deployment

### Step 1: Build and Push Container Image

```bash
# Build the image
podman build -t quay.io/your-org/insights-scheduler:latest .

# Push to registry
podman push quay.io/your-org/insights-scheduler:latest
```

### Step 2: Update Deployment Configuration

Edit `openshift/deployment.yaml` and update the image reference:

```yaml
spec:
  template:
    spec:
      containers:
      - name: scheduler
        image: quay.io/your-org/insights-scheduler:latest  # Update this line
```

### Step 3: Deploy to OpenShift

```bash
# Create/switch to namespace
oc new-project insights-scheduler

# Apply all resources
oc apply -f openshift/deployment.yaml

# Monitor deployment
oc rollout status deployment/insights-scheduler
```

## Configuration

### Environment Variables

The application can be configured using the following environment variables:

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `DB_PATH` | Path to SQLite database file | `/data/jobs.db` | No |
| `KAFKA_BROKERS` | Comma-separated list of Kafka brokers | `""` (disabled) | No |
| `KAFKA_TOPIC` | Kafka topic for export completion messages | `export-completions` | No |
| `PORT` | HTTP server port | `5000` | No |
| `EXPORT_SERVICE_ACCOUNT` | Export service account number | From secret | Yes |
| `EXPORT_SERVICE_ORG_ID` | Export service organization ID | From secret | Yes |

### Secrets Configuration

Update the secrets in `deployment.yaml`:

```bash
# Encode values in base64
echo -n "your-account-number" | base64
echo -n "your-org-id" | base64
```

Then update the secret in the YAML:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: insights-scheduler-secrets
data:
  EXPORT_SERVICE_ACCOUNT: "eW91ci1hY2NvdW50LW51bWJlcg=="  # your-account-number
  EXPORT_SERVICE_ORG_ID: "eW91ci1vcmctaWQ="  # your-org-id
```

### Kafka Configuration

To enable Kafka integration, update the ConfigMap:

```yaml
data:
  KAFKA_BROKERS: "kafka1.example.com:9092,kafka2.example.com:9092"
  KAFKA_TOPIC: "insights-export-completions"
```

## Storage

The application uses a PersistentVolumeClaim for the SQLite database:

- **Size**: 1Gi (adjustable)
- **Access Mode**: ReadWriteOnce
- **Storage Class**: `gp2` (update based on your cluster)

To use a different storage class:

```yaml
spec:
  storageClassName: fast-ssd  # Your storage class
```

## Networking

### Service

The application is exposed via a ClusterIP service on port 80, which forwards to container port 5000.

### Route

An OpenShift Route provides external access with TLS termination:

- **TLS**: Edge termination
- **Timeout**: 300 seconds
- **Insecure Traffic**: Redirected to HTTPS

## Security

### Pod Security

- **Non-root user**: Runs as UID 1001
- **Security context**: Restrictive settings
- **Capabilities**: All dropped
- **Privilege escalation**: Disabled

### RBAC

The deployment includes:

- ServiceAccount: `insights-scheduler-sa`
- Role: Limited permissions for pods, configmaps, secrets
- RoleBinding: Binds the ServiceAccount to the Role

## Monitoring and Health Checks

### Health Checks

- **Readiness Probe**: `GET /api/v1/jobs` every 10s
- **Liveness Probe**: `GET /api/v1/jobs` every 30s

### Resource Limits

```yaml
resources:
  requests:
    memory: "128Mi"
    cpu: "100m"
  limits:
    memory: "512Mi"
    cpu: "500m"
```

## Scaling Considerations

⚠️ **Important**: This application uses SQLite, which doesn't support multiple writers. Keep replicas at 1.

For production use with multiple replicas, consider:
1. Using PostgreSQL or MySQL instead of SQLite
2. Implementing database connection pooling
3. Adding read replicas for scaling reads

## Troubleshooting

### Common Issues

1. **Image Pull Errors**
   ```bash
   # Check if image exists and is accessible
   podman pull quay.io/your-org/insights-scheduler:latest
   
   # Verify image registry credentials
   oc get secret -n insights-scheduler
   ```

2. **Database Permission Issues**
   ```bash
   # Check PVC status
   oc get pvc -n insights-scheduler
   
   # Check pod logs
   oc logs deployment/insights-scheduler -n insights-scheduler
   ```

3. **Kafka Connection Issues**
   ```bash
   # Check if Kafka brokers are accessible
   oc exec deployment/insights-scheduler -- nc -zv kafka-broker 9092
   
   # Disable Kafka temporarily
   oc patch configmap insights-scheduler-config -p '{"data":{"KAFKA_BROKERS":""}}'
   ```

### Useful Commands

```bash
# View all resources
oc get all -n insights-scheduler

# Check pod logs
oc logs -f deployment/insights-scheduler -n insights-scheduler

# Get shell access to pod
oc exec -it deployment/insights-scheduler -- /bin/sh

# Check database file
oc exec deployment/insights-scheduler -- ls -la /data/

# Test API endpoint
ROUTE_URL=$(oc get route insights-scheduler-route -o jsonpath='{.spec.host}')
curl -X GET https://${ROUTE_URL}/api/v1/jobs

# Scale deployment (not recommended due to SQLite)
oc scale deployment/insights-scheduler --replicas=1
```

### Log Analysis

Look for these log messages:

- `"Database initialized successfully"` - SQLite is working
- `"Kafka producer initialized successfully"` - Kafka integration is active
- `"Cron scheduler started"` - Job scheduling is active
- `"Starting server on :5000"` - HTTP server is ready

## Production Considerations

1. **Database**: Consider migrating to PostgreSQL for production
2. **Monitoring**: Add Prometheus metrics and Grafana dashboards
3. **Backup**: Implement database backup strategy
4. **Security**: Use OpenShift security scanning and policies
5. **Updates**: Implement blue-green or rolling update strategy
6. **Resources**: Monitor and adjust resource requests/limits
7. **Networking**: Consider NetworkPolicies for security

## Support

For issues related to:
- **Application**: Check application logs and database connectivity
- **OpenShift**: Verify cluster permissions and resource availability
- **Kafka**: Check broker connectivity and topic configuration
- **Export Service**: Verify account credentials and service availability