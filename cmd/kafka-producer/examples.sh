#!/bin/bash

# Kafka Producer Examples
# Usage: ./examples.sh [example_name]

set -e

PRODUCER="./kafka-producer"
BROKERS="localhost:9092"
TOPIC="platform.notifications.ingress"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() {
    echo -e "${GREEN}[$(date +'%H:%M:%S')] $1${NC}"
}

warn() {
    echo -e "${YELLOW}[$(date +'%H:%M:%S')] WARNING: $1${NC}"
}

error() {
    echo -e "${RED}[$(date +'%H:%M:%S')] ERROR: $1${NC}"
}

# Check if producer exists
if [[ ! -f "$PRODUCER" ]]; then
    error "Kafka producer not found. Run: go build -o kafka-producer ./cmd/kafka-producer/"
    exit 1
fi

# Example 1: Basic notification message
example_basic_notification() {
    log "Example 1: Sending basic notification message"
    $PRODUCER \
        -brokers "$BROKERS" \
        -topic "$TOPIC" \
        -type "notification" \
        -account-id "123456" \
        -org-id "org-test" \
        -verbose
}

# Example 2: Export completion message
example_export_completion() {
    log "Example 2: Sending export completion message"
    $PRODUCER \
        -brokers "$BROKERS" \
        -topic "$TOPIC" \
        -type "export-completion" \
        -export-id "export-$(date +%s)" \
        -job-id "job-$(date +%s)" \
        -org-id "org-test" \
        -status "completed" \
        -download-url "https://example.com/exports/test" \
        -verbose
}

# Example 3: Failed export notification
example_failed_export() {
    log "Example 3: Sending failed export notification"
    $PRODUCER \
        -brokers "$BROKERS" \
        -topic "$TOPIC" \
        -type "export-completion" \
        -export-id "failed-export-$(date +%s)" \
        -job-id "job-$(date +%s)" \
        -org-id "org-test" \
        -status "failed" \
        -error-msg "Export processing failed due to invalid data format" \
        -verbose
}

# Example 4: Multiple messages with interval
example_multiple_messages() {
    log "Example 4: Sending multiple messages with 1-second intervals"
    $PRODUCER \
        -brokers "$BROKERS" \
        -topic "$TOPIC" \
        -type "notification" \
        -count 3 \
        -interval 1s \
        -org-id "org-batch-test" \
        -verbose
}

# Example 5: Custom JSON message
example_custom_message() {
    log "Example 5: Sending custom JSON message"
    
    custom_json='{
        "version": "v1.2.0",
        "bundle": "custom-bundle",
        "application": "test-app",
        "event_type": "custom-event",
        "account_id": "999999",
        "org_id": "custom-org",
        "context": {
            "custom_field": "custom_value",
            "test_mode": true,
            "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"
        }
    }'
    
    $PRODUCER \
        -brokers "$BROKERS" \
        -topic "$TOPIC" \
        -type "custom" \
        -custom "$custom_json" \
        -verbose
}

# Example 6: Load testing
example_load_test() {
    log "Example 6: Load testing - 10 messages with 100ms intervals"
    warn "This will send 10 messages rapidly. Make sure your Kafka can handle the load."
    
    $PRODUCER \
        -brokers "$BROKERS" \
        -topic "$TOPIC" \
        -type "notification" \
        -count 10 \
        -interval 100ms \
        -org-id "org-load-test" \
        -verbose
}

# Example 7: Different bundles and applications
example_different_apps() {
    log "Example 7: Testing different bundles and applications"
    
    # Inventory application
    log "  -> Sending inventory notification"
    $PRODUCER \
        -brokers "$BROKERS" \
        -topic "$TOPIC" \
        -type "notification" \
        -bundle "rhel" \
        -application "inventory" \
        -event-type "system-registered" \
        -org-id "org-inventory"
    
    # Subscriptions application  
    log "  -> Sending subscriptions notification"
    $PRODUCER \
        -brokers "$BROKERS" \
        -topic "$TOPIC" \
        -type "notification" \
        -bundle "rhel" \
        -application "subscriptions" \
        -event-type "subscription-changed" \
        -org-id "org-subscriptions"
    
    # Insights application
    log "  -> Sending insights notification" 
    $PRODUCER \
        -brokers "$BROKERS" \
        -topic "$TOPIC" \
        -type "notification" \
        -bundle "rhel" \
        -application "insights" \
        -event-type "recommendation-created" \
        -org-id "org-insights"
}

# Example 8: Test connectivity only
example_connectivity_test() {
    log "Example 8: Testing Kafka connectivity"
    $PRODUCER \
        -brokers "$BROKERS" \
        -topic "$TOPIC" \
        -type "notification" \
        -count 1 \
        -org-id "org-connectivity-test" \
        -verbose
}

# Example 9: Environment variable usage
example_env_vars() {
    log "Example 9: Using environment variables"
    
    export KAFKA_BROKERS="$BROKERS"
    export KAFKA_TOPIC="$TOPIC" 
    export ACCOUNT_ID="env-account-123"
    export ORG_ID="env-org-456"
    
    log "  Environment variables set:"
    log "    KAFKA_BROKERS=$KAFKA_BROKERS"
    log "    KAFKA_TOPIC=$KAFKA_TOPIC"
    log "    ACCOUNT_ID=$ACCOUNT_ID"
    log "    ORG_ID=$ORG_ID"
    
    # Note: The current producer doesn't read env vars, but this shows the pattern
    $PRODUCER \
        -brokers "$KAFKA_BROKERS" \
        -topic "$KAFKA_TOPIC" \
        -type "notification" \
        -account-id "$ACCOUNT_ID" \
        -org-id "$ORG_ID" \
        -verbose
}

# Help function
show_help() {
    echo "Kafka Producer Examples"
    echo "======================="
    echo
    echo "Usage: $0 [example_name]"
    echo
    echo "Available examples:"
    echo "  basic_notification    - Send a basic notification message"
    echo "  export_completion     - Send export completion message"
    echo "  failed_export         - Send failed export notification"
    echo "  multiple_messages     - Send multiple messages with intervals"
    echo "  custom_message        - Send custom JSON message"
    echo "  load_test            - Load testing with rapid messages"
    echo "  different_apps       - Test different applications and bundles"
    echo "  connectivity_test    - Test Kafka connectivity"
    echo "  env_vars             - Example using environment variables"
    echo "  all                  - Run all examples (be careful!)"
    echo
    echo "Configuration:"
    echo "  BROKERS: $BROKERS"
    echo "  TOPIC: $TOPIC"
    echo
    echo "Examples:"
    echo "  $0 basic_notification"
    echo "  $0 load_test"
    echo "  BROKERS=kafka:9092 $0 connectivity_test"
}

# Main execution
case "${1:-help}" in
    "basic_notification")
        example_basic_notification
        ;;
    "export_completion")
        example_export_completion
        ;;
    "failed_export")
        example_failed_export
        ;;
    "multiple_messages")
        example_multiple_messages
        ;;
    "custom_message")
        example_custom_message
        ;;
    "load_test")
        example_load_test
        ;;
    "different_apps")
        example_different_apps
        ;;
    "connectivity_test")
        example_connectivity_test
        ;;
    "env_vars")
        example_env_vars
        ;;
    "all")
        warn "Running ALL examples - this will send many messages!"
        read -p "Are you sure? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            example_connectivity_test
            sleep 1
            example_basic_notification
            sleep 1
            example_export_completion
            sleep 1
            example_failed_export
            sleep 1
            example_custom_message
            sleep 1
            example_different_apps
            sleep 2
            example_multiple_messages
            log "All examples completed!"
        else
            log "Cancelled."
        fi
        ;;
    "help"|*)
        show_help
        ;;
esac