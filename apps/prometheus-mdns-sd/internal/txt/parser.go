package txt

import (
	"log/slog"
	"strings"
)

// ParsedTXT represents the parsed contents of mDNS TXT records
type ParsedTXT struct {
	Path     string            // metrics path from path= field
	Labels   map[string]string // labels from label:KEY=VALUE fields
	Meta     map[string]string // meta labels from meta:KEY=VALUE fields
	NoScrape bool              // true if NO_SCRAPE=true is present
	Skipped  []string          // invalid/skipped TXT records for logging
}

// Parse processes TXT records according to the prometheus mDNS specification.
// It handles:
// - path=VALUE -> sets Path
// - label:KEY=VALUE -> adds to Labels map
// - meta:KEY=VALUE -> adds to Meta map (will become __meta_KEY)
// - NO_SCRAPE=true -> sets NoScrape=true
// - Invalid/malformed records are added to Skipped for debug logging
func Parse(txtRecords []string) *ParsedTXT {
	parsed := &ParsedTXT{
		Labels:  make(map[string]string),
		Meta:    make(map[string]string),
		Skipped: make([]string, 0),
	}

	for _, record := range txtRecords {
		if record == "" {
			continue
		}

		// Handle NO_SCRAPE flag
		if record == "NO_SCRAPE=true" {
			parsed.NoScrape = true
			continue
		}

		// Parse KEY=VALUE format
		parts := strings.SplitN(record, "=", 2)
		if len(parts) != 2 {
			parsed.Skipped = append(parsed.Skipped, record)
			continue
		}

		key, value := parts[0], parts[1]

		switch {
		case key == "path":
			parsed.Path = value
		case strings.HasPrefix(key, "label:"):
			labelKey := strings.TrimPrefix(key, "label:")
			if labelKey != "" && isValidLabelName(labelKey) {
				parsed.Labels[labelKey] = value
			} else {
				parsed.Skipped = append(parsed.Skipped, record)
			}
		case strings.HasPrefix(key, "meta:"):
			metaKey := strings.TrimPrefix(key, "meta:")
			if metaKey != "" && isValidLabelName(metaKey) {
				parsed.Meta[metaKey] = value
			} else {
				parsed.Skipped = append(parsed.Skipped, record)
			}
		default:
			parsed.Skipped = append(parsed.Skipped, record)
		}
	}

	return parsed
}

// LogSkipped logs any skipped/invalid TXT records at debug level
func (p *ParsedTXT) LogSkipped(logger *slog.Logger, serviceName string) {
	if len(p.Skipped) > 0 {
		logger.Debug("skipped invalid TXT records",
			"service", serviceName,
			"skipped_records", p.Skipped)
	}
}

// isValidLabelName validates Prometheus label names.
// Valid names must match [a-zA-Z_:][a-zA-Z0-9_:]*
func isValidLabelName(name string) bool {
	if name == "" {
		return false
	}

	// First character must be letter, underscore, or colon
	first := name[0]
	if !((first >= 'a' && first <= 'z') ||
		(first >= 'A' && first <= 'Z') ||
		first == '_' || first == ':') {
		return false
	}

	// Remaining characters must be letter, digit, underscore, or colon
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !((c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '_' || c == ':') {
			return false
		}
	}

	return true
}
