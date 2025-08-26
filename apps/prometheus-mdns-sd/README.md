# Prometheus mDNS Service Discovery

A pair of Go programs that provide Prometheus service discovery by querying mDNS for `_prometheus-http._tcp` services. The programs handle proper deduplication, TXT record parsing, and TTL management.

## Features

- **IPv4/IPv6 Deduplication**: When a service advertises both IPv4 and IPv6 addresses, prefer IPv4 to avoid duplicate scraping
- **TXT Record Processing**: Support for `path=`, `label:KEY=VALUE`, `meta:KEY=VALUE`, and `NO_SCRAPE=true`
- **TTL Management**: Respects mDNS TTLs for automatic target removal (5-minute default)
- **Label Merging**: Last-write-wins semantics for conflicting labels on same endpoint
- **Two Output Formats**: HTTP service discovery server and file-based discovery

## Programs

### http-sd

HTTP service discovery server that exposes a JSON endpoint for Prometheus.

**Usage:**
```bash
go run ./cmd/http-sd -port 8080 -debug
```

**Endpoints:**
- `GET /targets` - Prometheus service discovery JSON
- `GET /health` - Health check endpoint
- `GET /metrics` - Basic metrics about discovered targets

**Flags:**
- `-port int`: HTTP server port (default 8080)
- `-addr string`: HTTP server address (empty for all interfaces)
- `-debug`: Enable debug logging

### file-sd

File-based service discovery that writes JSON target files to disk.

**Usage:**
```bash
go run ./cmd/file-sd -output /etc/prometheus/mdns_targets.json -interval 30s -debug
```

**Flags:**
- `-output string`: Output file path for target groups (default "prometheus_targets.json")
- `-interval duration`: Write interval for target file updates (default 30s)
- `-debug`: Enable debug logging

## mDNS TXT Record Format

The discovery service processes TXT records according to these rules:

### Supported Fields

- `path=/metrics` → Sets `__meta_metrics_path=/metrics` label
- `label:env=prod` → Sets `env=prod` label directly  
- `meta:datacenter=us-east` → Sets `__meta_datacenter=us-east` label
- `NO_SCRAPE=true` → Excludes target from discovery entirely

### Label Processing Rules

1. **Last-write-wins**: If multiple TXT records set the same label, the last one wins
2. **Invalid labels ignored**: Labels not matching `[a-zA-Z_:][a-zA-Z0-9_:]*` are skipped
3. **Debug logging**: Invalid/skipped TXT records are logged at debug level

### Example mDNS Advertisement

```bash
# Using avahi-publish-service
avahi-publish-service "My Service" _prometheus-http._tcp 9100 \
  "path=/api/metrics" \
  "label:job=my-service" \
  "label:env=production" \
  "meta:region=us-west"
```

This creates a target with:
- Target: `192.168.1.100:9100` (assuming that's the host IP)  
- Labels: `job=my-service`, `env=production`
- Meta labels: `__meta_metrics_path=/api/metrics`, `__meta_region=us-west`

## Prometheus Configuration

### HTTP Service Discovery

```yaml
scrape_configs:
  - job_name: 'mdns-http-sd'
    http_sd_configs:
      - url: 'http://localhost:8080/targets'
        refresh_interval: 30s
    
    # Use the metrics path from mDNS TXT records
    relabel_configs:
      - source_labels: [__meta_metrics_path]
        target_label: __metrics_path__
        
      # Optional: Add discovered meta labels as regular labels
      - source_labels: [__meta_region]
        target_label: region
```

### File Service Discovery

```yaml
scrape_configs:
  - job_name: 'mdns-file-sd'
    file_sd_configs:
      - files: ['/etc/prometheus/mdns_targets.json']
        refresh_interval: 30s
    
    # Use the metrics path from mDNS TXT records  
    relabel_configs:
      - source_labels: [__meta_metrics_path]
        target_label: __metrics_path__
        
      # Optional: Add discovered meta labels as regular labels
      - source_labels: [__meta_region]  
        target_label: region
```

### Important Notes on __metrics_path__

Since Prometheus HTTP/file service discovery doesn't support per-target metrics paths, the `path=` TXT record is converted to the `__meta_metrics_path` label. **You must use relabel_config to set `__metrics_path__`** as shown above, otherwise Prometheus will use the default `/metrics` path for all targets.

## IPv4/IPv6 Deduplication

When a service advertises both IPv4 and IPv6 addresses for the same (port, path) combination:

1. **IPv4 is preferred** - Only the IPv4 address will be included as a target
2. **Labels are merged** - All labels from both advertisements are combined
3. **Documented behavior** - This prevents accidental duplicate scraping when devices advertise on both protocols

Example: If a device advertises:
- IPv6: `[2001:db8::1]:9100/metrics` with `label:env=prod`
- IPv4: `192.168.1.10:9100/metrics` with `label:job=node`

Result: Single target `192.168.1.10:9100` with labels `env=prod` and `job=node`.

## Building

```bash
# Build HTTP service discovery server
go build -o prometheus-mdns-http-sd ./cmd/http-sd

# Build file service discovery server  
go build -o prometheus-mdns-file-sd ./cmd/file-sd
```

## Testing

```bash
# Run all tests
go test -v ./...

# Run tests with race detection
go test -race ./...

# Test with debug logging
go test -v ./internal/targets -debug
```

## Example Output

### JSON Target Format

Both programs output the same JSON format compatible with Prometheus:

```json
[
  {
    "targets": ["192.168.1.100:9100"],
    "labels": {
      "job": "node-exporter",
      "env": "production", 
      "__meta_metrics_path": "/metrics",
      "__meta_region": "us-west"
    }
  },
  {
    "targets": ["192.168.1.101:9200"],
    "labels": {
      "job": "elasticsearch",
      "__meta_metrics_path": "/stats"
    }
  }
]
```

## Limitations

- **mDNS scope**: Only discovers services on the local network segment
- **TTL handling**: Uses 5-minute default TTL since hashicorp/mdns doesn't expose actual TTL values
- **No authentication**: Programs don't support mTLS or authentication (run behind reverse proxy if needed)

## Architecture

```
┌─────────────┐    ┌──────────────┐    ┌─────────────┐
│   mDNS      │───▶│   Discovery  │───▶│   Target    │
│   Client    │    │   Client     │    │   Manager   │
└─────────────┘    └──────────────┘    └─────────────┘
                                              │
                   ┌─────────────────────────┼─────────────────────────┐
                   ▼                         ▼                         ▼
            ┌──────────────┐         ┌──────────────┐         ┌──────────────┐
            │ HTTP Server  │         │ File Writer  │         │ TXT Parser   │
            │ (/targets)   │         │ (JSON files) │         │ (labels)     │
            └──────────────┘         └──────────────┘         └──────────────┘
```