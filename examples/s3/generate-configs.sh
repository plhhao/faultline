#!/bin/sh
set -eu

case "${S3_TEST_HOST:-}" in
  ""|*[!a-zA-Z0-9.-]*) echo "S3_TEST_HOST must be a hostname" >&2; exit 1 ;;
esac

CONFIG_DIR=${CONFIG_DIR:-/configs}
mkdir -p "$CONFIG_DIR"
cat > "$CONFIG_DIR/baseline.yaml" <<EOF
api_version: faultline/v1alpha1
runtime:
  max_inflight_requests: 32
  request_timeout: 30s
proxies:
  - id: s3
    protocol: tcp
    listen: 0.0.0.0:443
    upstream: tcp://${S3_TEST_HOST}:443
    rules: []
EOF

write_rule() {
  name="$1"
  action="$2"
  extras="$3"
  cat > "$CONFIG_DIR/${name}.yaml" <<EOF
api_version: faultline/v1alpha1
runtime:
  max_inflight_requests: 32
  request_timeout: 30s
proxies:
  - id: s3
    protocol: tcp
    listen: 0.0.0.0:443
    upstream: tcp://${S3_TEST_HOST}:443
    rules:
      - id: ${name}
        enabled: true
        match: {}
        select: {probability: 1}
        fault:
          phase: on_transfer
          action: ${action}
${extras}
EOF
}

write_rule disconnect close_connection ""
write_rule cut close_connection '          after_bytes: 4096
          trigger_direction: client_to_upstream'
write_rule hold_response hold '          direction: upstream_to_client
          max_duration: 5s
          after_bytes: 4096
          trigger_direction: client_to_upstream'
write_rule hold_upload hold '          direction: client_to_upstream
          max_duration: 5s
          after_bytes: 4096
          trigger_direction: client_to_upstream'
write_rule slow_upload throttle '          direction: client_to_upstream
          bytes_per_second: 32768
          after_bytes: 4096
          trigger_direction: client_to_upstream'
write_rule slow_response throttle '          direction: upstream_to_client
          bytes_per_second: 32768
          after_bytes: 4096
          trigger_direction: upstream_to_client'

cat > "$CONFIG_DIR/delay_connect.yaml" <<EOF
api_version: faultline/v1alpha1
runtime:
  max_inflight_requests: 32
  request_timeout: 30s
proxies:
  - id: s3
    protocol: tcp
    listen: 0.0.0.0:443
    upstream: tcp://${S3_TEST_HOST}:443
    rules:
      - id: delay_connect
        enabled: true
        match: {}
        select: {probability: 1}
        fault:
          phase: on_connect
          action: delay_connect
          duration: 2s
EOF
