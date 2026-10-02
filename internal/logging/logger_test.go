package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/infrastruture/sims/internal/logging"
)

func TestRedactionHandler(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := logging.NewLogger(buf, slog.LevelInfo)

	logger.Info("connecting to vCenter",
		slog.String("vcenter_url", "https://administrator%40vsphere.local:SuperSecretPass123@vcenter.corp.local/sdk"),
		slog.String("password", "SuperSecretPass123"),
		slog.String("auth_token", "jwt-token-abcdef123456"),
		slog.String("api_key", "secret-key-999"),
		slog.String("user", "svc-collector"),
		slog.Int("attempt", 1),
	)

	logged := buf.String()
	t.Logf("Logged output: %s", logged)

	if strings.Contains(logged, "SuperSecretPass123") {
		t.Errorf("expected secret password to be redacted, found in log output")
	}
	if strings.Contains(logged, "jwt-token-abcdef123456") {
		t.Errorf("expected auth_token to be redacted, found in log output")
	}
	if strings.Contains(logged, "secret-key-999") {
		t.Errorf("expected api_key to be redacted, found in log output")
	}

	var data map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &data); err != nil {
		t.Fatalf("failed to parse log JSON: %v", err)
	}

	if data["password"] != "[REDACTED]" {
		t.Errorf("expected password attribute to be [REDACTED], got %v", data["password"])
	}
	if data["auth_token"] != "[REDACTED]" {
		t.Errorf("expected auth_token attribute to be [REDACTED], got %v", data["auth_token"])
	}
	if data["user"] != "svc-collector" {
		t.Errorf("expected user attribute to remain untouched, got %v", data["user"])
	}
}

func TestRedactURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "https://admin:mypassword@vcenter.local/sdk",
			expected: "https://admin:[REDACTED]@vcenter.local/sdk",
		},
		{
			input:    "https://vcenter.local/sdk",
			expected: "https://vcenter.local/sdk",
		},
		{
			input:    "postgres://collector:pgpass@localhost:5432/sims?sslmode=disable",
			expected: "postgres://collector:[REDACTED]@localhost:5432/sims?sslmode=disable",
		},
	}

	for _, tt := range tests {
		got := logging.RedactURL(tt.input)
		if got != tt.expected {
			t.Errorf("RedactURL(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}
