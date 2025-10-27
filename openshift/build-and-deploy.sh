#!/bin/bash

# OpenShift Build and Deploy Script for Insights Scheduler
set -e

# Configuration
NAMESPACE="insights-scheduler"
APP_NAME="insights-scheduler"
IMAGE_REGISTRY="${IMAGE_REGISTRY:-quay.io}"
IMAGE_ORG="${IMAGE_ORG:-your-org}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
IMAGE_NAME="${IMAGE_REGISTRY}/${IMAGE_ORG}/${APP_NAME}:${IMAGE_TAG}"

echo "=== OpenShift Deployment Script for Insights Scheduler ==="
echo "Namespace: ${NAMESPACE}"
echo "Image: ${IMAGE_NAME}"
echo

# Function to check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Check prerequisites
echo "Checking prerequisites..."
if ! command_exists oc; then
    echo "Error: OpenShift CLI (oc) is not installed"
    exit 1
fi

if ! command_exists podman && ! command_exists docker; then
    echo "Error: Neither podman nor docker is installed"
    exit 1
fi

# Determine container tool
if command_exists podman; then
    CONTAINER_TOOL="podman"
else
    CONTAINER_TOOL="docker"
fi

echo "Using container tool: ${CONTAINER_TOOL}"

# Check if logged into OpenShift
if ! oc whoami >/dev/null 2>&1; then
    echo "Error: Not logged into OpenShift. Please run 'oc login' first."
    exit 1
fi

echo "Logged into OpenShift as: $(oc whoami)"
echo

# Build the container image
echo "Building container image..."
cd "$(dirname "$0")/.."  # Go to project root

${CONTAINER_TOOL} build -t ${IMAGE_NAME} .

if [ $? -ne 0 ]; then
    echo "Error: Failed to build container image"
    exit 1
fi

echo "Container image built successfully: ${IMAGE_NAME}"
echo

# Push the image to registry
echo "Pushing image to registry..."
${CONTAINER_TOOL} push ${IMAGE_NAME}

if [ $? -ne 0 ]; then
    echo "Error: Failed to push container image"
    exit 1
fi

echo "Image pushed successfully to: ${IMAGE_NAME}"
echo

# Create or switch to namespace
echo "Creating/switching to namespace: ${NAMESPACE}"
oc new-project ${NAMESPACE} 2>/dev/null || oc project ${NAMESPACE}

# Update the deployment YAML with the correct image
echo "Updating deployment configuration..."
DEPLOYMENT_FILE="/tmp/deployment-${APP_NAME}.yaml"
cp openshift/deployment.yaml ${DEPLOYMENT_FILE}

# Replace the image placeholder with actual image name
sed -i.bak "s|quay.io/your-org/insights-scheduler:latest|${IMAGE_NAME}|g" ${DEPLOYMENT_FILE}

# Apply the deployment
echo "Applying OpenShift resources..."
oc apply -f ${DEPLOYMENT_FILE}

if [ $? -ne 0 ]; then
    echo "Error: Failed to apply OpenShift resources"
    exit 1
fi

# Wait for deployment to be ready
echo "Waiting for deployment to be ready..."
oc rollout status deployment/${APP_NAME} -n ${NAMESPACE} --timeout=300s

if [ $? -ne 0 ]; then
    echo "Error: Deployment failed or timed out"
    echo "Check the deployment status with: oc get pods -n ${NAMESPACE}"
    exit 1
fi

# Get the route URL
echo "Getting application URL..."
ROUTE_URL=$(oc get route ${APP_NAME}-route -n ${NAMESPACE} -o jsonpath='{.spec.host}' 2>/dev/null)

if [ -n "${ROUTE_URL}" ]; then
    echo
    echo "=== Deployment Successful ==="
    echo "Application URL: https://${ROUTE_URL}"
    echo "API Documentation: https://${ROUTE_URL}/api/v1/jobs"
    echo
    echo "To test the deployment:"
    echo "curl -X GET https://${ROUTE_URL}/api/v1/jobs"
    echo
else
    echo "Warning: Could not retrieve route URL"
fi

echo "=== Deployment Commands ==="
echo "View pods:        oc get pods -n ${NAMESPACE}"
echo "View logs:        oc logs deployment/${APP_NAME} -n ${NAMESPACE}"
echo "View service:     oc get svc -n ${NAMESPACE}"
echo "View route:       oc get route -n ${NAMESPACE}"
echo "Scale deployment: oc scale deployment/${APP_NAME} --replicas=1 -n ${NAMESPACE}"
echo

# Clean up temp file
rm -f ${DEPLOYMENT_FILE} ${DEPLOYMENT_FILE}.bak

echo "Deployment completed successfully!"