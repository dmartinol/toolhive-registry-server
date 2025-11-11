package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSourceFormat_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		format   SourceFormat
		expected string
	}{
		{
			name:     "toolhive format",
			format:   SourceFormatToolHive,
			expected: "toolhive",
		},
		{
			name:     "upstream format",
			format:   SourceFormatUpstream,
			expected: "upstream",
		},
		{
			name:     "empty format",
			format:   "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.format.String()
			if result != tt.expected {
				t.Errorf("String() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSourceFormat_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		format    SourceFormat
		wantError bool
	}{
		{
			name:      "valid toolhive format",
			format:    SourceFormatToolHive,
			wantError: false,
		},
		{
			name:      "valid upstream format",
			format:    SourceFormatUpstream,
			wantError: false,
		},
		{
			name:      "empty format (invalid after unmarshal sets default)",
			format:    "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.format.Validate()
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestSourceFormat_UnmarshalYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		yamlContent   string
		expectedValue SourceFormat
		wantError     bool
	}{
		{
			name:          "valid toolhive format",
			yamlContent:   "format: toolhive",
			expectedValue: SourceFormatToolHive,
			wantError:     false,
		},
		{
			name:          "valid upstream format",
			yamlContent:   "format: upstream",
			expectedValue: SourceFormatUpstream,
			wantError:     false,
		},
		{
			name:          "empty format (defaults to toolhive)",
			yamlContent:   "format: \"\"",
			expectedValue: SourceFormatToolHive,
			wantError:     false,
		},
		{
			name:          "missing format field (defaults to toolhive)",
			yamlContent:   "type: file",
			expectedValue: SourceFormatToolHive,
			wantError:     false,
		},
		{
			name:        "invalid format",
			yamlContent: "format: invalid",
			wantError:   true,
		},
		{
			name:        "numeric format (invalid)",
			yamlContent: "format: 123",
			wantError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var result SourceConfig
			err := yaml.Unmarshal([]byte(tt.yamlContent), &result)

			if (err != nil) != tt.wantError {
				t.Errorf("UnmarshalYAML() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if !tt.wantError && result.Format != tt.expectedValue {
				t.Errorf("UnmarshalYAML() got = %v, want %v", result.Format, tt.expectedValue)
			}
		})
	}
}

func TestSourceFormat_InSourceConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		yamlContent   string
		expectedValue SourceFormat
		wantError     bool
	}{
		{
			name: "full config with toolhive format",
			yamlContent: `
source:
  type: file
  format: toolhive
  file:
    path: /data/registry.json
`,
			expectedValue: SourceFormatToolHive,
			wantError:     false,
		},
		{
			name: "full config with upstream format",
			yamlContent: `
source:
  type: api
  format: upstream
  api:
    endpoint: http://example.com
`,
			expectedValue: SourceFormatUpstream,
			wantError:     false,
		},
		{
			name: "config without format (empty treated as toolhive)",
			yamlContent: `
source:
  type: file
  file:
    path: /data/registry.json
`,
			expectedValue: SourceFormatToolHive,
			wantError:     false,
		},
		{
			name: "config with invalid format",
			yamlContent: `
source:
  type: file
  format: badformat
  file:
    path: /data/registry.json
`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			type testConfig struct {
				Source SourceConfig `yaml:"source"`
			}

			var result testConfig
			err := yaml.Unmarshal([]byte(tt.yamlContent), &result)

			if (err != nil) != tt.wantError {
				t.Errorf("UnmarshalYAML() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if !tt.wantError && result.Source.Format != tt.expectedValue {
				t.Errorf("Format got = %v, want %v", result.Source.Format, tt.expectedValue)
			}
		})
	}
}

func TestSourceFormat_TypeSafety(t *testing.T) {
	t.Parallel()

	// Ensure typed constants can be used in comparisons
	format := SourceFormatToolHive

	if format != SourceFormatToolHive {
		t.Error("Typed constant comparison failed")
	}

	// Ensure they work in switch statements
	switch format {
	case SourceFormatToolHive:
		// Expected path
	case SourceFormatUpstream:
		t.Error("Wrong case matched")
	default:
		t.Error("Default case should not be reached")
	}

	// Ensure empty string comparisons work
	var emptyFormat SourceFormat
	if emptyFormat != "" {
		t.Error("Empty format should equal empty string")
	}
}
