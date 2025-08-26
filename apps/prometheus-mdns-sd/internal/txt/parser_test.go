package txt

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name       string
		txtRecords []string
		expected   *ParsedTXT
	}{
		{
			name:       "empty records",
			txtRecords: []string{},
			expected: &ParsedTXT{
				Labels:  map[string]string{},
				Meta:    map[string]string{},
				Skipped: []string{},
			},
		},
		{
			name:       "path only",
			txtRecords: []string{"path=/metrics"},
			expected: &ParsedTXT{
				Path:    "/metrics",
				Labels:  map[string]string{},
				Meta:    map[string]string{},
				Skipped: []string{},
			},
		},
		{
			name:       "NO_SCRAPE flag",
			txtRecords: []string{"NO_SCRAPE=true", "path=/metrics"},
			expected: &ParsedTXT{
				Path:     "/metrics",
				Labels:   map[string]string{},
				Meta:     map[string]string{},
				NoScrape: true,
				Skipped:  []string{},
			},
		},
		{
			name:       "labels and meta",
			txtRecords: []string{"label:env=prod", "label:region=us-east", "meta:datacenter=dc1"},
			expected: &ParsedTXT{
				Labels:  map[string]string{"env": "prod", "region": "us-east"},
				Meta:    map[string]string{"datacenter": "dc1"},
				Skipped: []string{},
			},
		},
		{
			name:       "invalid records",
			txtRecords: []string{"version=2.0", "invalid", "=malformed", "label:=empty", "label:123invalid=value"},
			expected: &ParsedTXT{
				Labels:  map[string]string{},
				Meta:    map[string]string{},
				Skipped: []string{"version=2.0", "invalid", "=malformed", "label:=empty", "label:123invalid=value"},
			},
		},
		{
			name:       "mixed valid and invalid",
			txtRecords: []string{"path=/api/metrics", "label:job=node-exporter", "version=1.0", "meta:zone=us-west"},
			expected: &ParsedTXT{
				Path:    "/api/metrics",
				Labels:  map[string]string{"job": "node-exporter"},
				Meta:    map[string]string{"zone": "us-west"},
				Skipped: []string{"version=1.0"},
			},
		},
		{
			name:       "last write wins",
			txtRecords: []string{"label:env=staging", "label:env=prod", "path=/old", "path=/new"},
			expected: &ParsedTXT{
				Path:    "/new",
				Labels:  map[string]string{"env": "prod"},
				Meta:    map[string]string{},
				Skipped: []string{},
			},
		},
		{
			name:       "empty strings ignored",
			txtRecords: []string{"", "path=/metrics", "", "label:env=prod", ""},
			expected: &ParsedTXT{
				Path:    "/metrics",
				Labels:  map[string]string{"env": "prod"},
				Meta:    map[string]string{},
				Skipped: []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.txtRecords)

			if result.Path != tt.expected.Path {
				t.Errorf("Path = %v, want %v", result.Path, tt.expected.Path)
			}

			if result.NoScrape != tt.expected.NoScrape {
				t.Errorf("NoScrape = %v, want %v", result.NoScrape, tt.expected.NoScrape)
			}

			if !reflect.DeepEqual(result.Labels, tt.expected.Labels) {
				t.Errorf("Labels = %v, want %v", result.Labels, tt.expected.Labels)
			}

			if !reflect.DeepEqual(result.Meta, tt.expected.Meta) {
				t.Errorf("Meta = %v, want %v", result.Meta, tt.expected.Meta)
			}

			if !reflect.DeepEqual(result.Skipped, tt.expected.Skipped) {
				t.Errorf("Skipped = %v, want %v", result.Skipped, tt.expected.Skipped)
			}
		})
	}
}

func TestIsValidLabelName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty string", "", false},
		{"valid simple", "job", true},
		{"valid with underscore", "job_name", true},
		{"valid with colon", "__name__", true},
		{"valid with numbers", "http_2xx", true},
		{"starts with number", "2xx_count", false},
		{"starts with dash", "-invalid", false},
		{"has space", "job name", false},
		{"has dot", "job.name", false},
		{"has dash", "job-name", false},
		{"complex valid", "_internal:job_2xx", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidLabelName(tt.input)
			if result != tt.expected {
				t.Errorf("isValidLabelName(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
