#!/bin/sh

curl -X 'PUT' 'http://swatch-tally-service:8000/api/rhsm-subscriptions/v1/opt-in' \
-H 'accept: application/vnd.api+json' \
  -H "x-rh-identity: $(echo -n '{"identity":{"account_number":"","type":"User","user":{"is_org_admin":true},"internal":{"org_id":"000001"}}}' | base64 -w 0)"

curl -X PUT "http://swatch-tally-service:8000/api/rhsm-subscriptions/v1/internal/rpc/tally/snapshots/000001" \
    -H "x-rh-swatch-psk: placeholder" \
    -H "x-rh-swatch-synchronous-request: false"

curl -X GET "http://swatch-api-service:8000/api/rhsm-subscriptions/v1/instances/products/RHEL%20for%20x86" \
    -H "x-rh-identity: $(echo -n '{"identity":{"type":"User","internal":{"org_id":"000001"}}}' | base64 -w 0)"
