#!/bin/bash
# Example script showing how to advertise a Prometheus-scrapeable service via mDNS
# Requires avahi-utils package on most Linux distributions

set -e

SERVICE_NAME="${1:-MyService}"
PORT="${2:-9100}"  
METRICS_PATH="${3:-/metrics}"

echo "Advertising Prometheus service via mDNS..."
echo "  Service: $SERVICE_NAME"  
echo "  Port: $PORT"
echo "  Metrics Path: $METRICS_PATH"
echo ""
echo "TXT records will include:"
echo "  - path=$METRICS_PATH (sets __meta_metrics_path)"
echo "  - label:job=demo-service (sets job label)"  
echo "  - label:env=development (sets env label)"
echo "  - meta:region=local (sets __meta_region)" 
echo ""
echo "Press Ctrl+C to stop advertising..."
echo ""

# Advertise the service with comprehensive TXT records
exec avahi-publish-service \
  "$SERVICE_NAME" \
  _prometheus-http._tcp \
  "$PORT" \
  "path=$METRICS_PATH" \
  "label:job=demo-service" \
  "label:env=development" \
  "meta:region=local" \
  "meta:datacenter=homelab"

# Example usage:
#   ./advertise-service.sh "Node Exporter" 9100 "/metrics"  
#   ./advertise-service.sh "Custom App" 8080 "/api/metrics"

# To test NO_SCRAPE functionality:
#   avahi-publish-service "Ignored" _prometheus-http._tcp 9999 "NO_SCRAPE=true"