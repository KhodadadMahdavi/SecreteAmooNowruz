package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadWithLookupValid(t *testing.T) {
	t.Helper()
	lookup := mapLookup(map[string]string{
		"APP_ENV":                 "production",
		"PORT":                    "9090",
		"DB_DSN":                  "postgres://user:pass@localhost:5432/secrete",
		"SESSION_SECRET":          "12345678901234567890123456789012",
		"SESSION_TTL":             "48h",
		"SESSION_COOKIE_NAME":     "samn_session",
		"STORAGE_ENDPOINT":        "127.0.0.1:9000",
		"STORAGE_REGION":          "eu-central-1",
		"STORAGE_BUCKET":          "samn",
		"STORAGE_ACCESS_KEY":      "key",
		"STORAGE_SECRET_KEY":      "secret",
		"STORAGE_USE_SSL":         "true",
		"ADMIN_SEED_ENABLED":      "true",
		"ADMIN_SEED_USERNAME":     "admin",
		"ADMIN_SEED_PASSWORD":     "adminpass",
		"ADMIN_SEED_DISPLAY_NAME": "Administrator",
	})

	cfg, err := loadWithLookup(lookup)
	if err != nil {
		t.Fatalf("loadWithLookup() error = %v", err)
	}

	if cfg.HTTP.Port != 9090 {
		t.Fatalf("port = %d, want 9090", cfg.HTTP.Port)
	}
	if cfg.Session.TTL != 48*time.Hour {
		t.Fatalf("session ttl = %s, want 48h", cfg.Session.TTL)
	}
	if !cfg.Storage.UseSSL {
		t.Fatalf("storage use ssl = false, want true")
	}
	if cfg.AdminSeed.Username != "admin" {
		t.Fatalf("admin seed username = %q, want admin", cfg.AdminSeed.Username)
	}
}

func TestLoadWithLookupValidationError(t *testing.T) {
	t.Helper()
	lookup := mapLookup(map[string]string{
		"PORT":               "abc",
		"SESSION_SECRET":     "short",
		"STORAGE_USE_SSL":    "nope",
		"ADMIN_SEED_ENABLED": "true",
	})

	_, err := loadWithLookup(lookup)
	if err == nil {
		t.Fatal("loadWithLookup() error = nil, want validation error")
	}

	text := err.Error()
	mustContain := []string{
		"PORT must be an integer",
		"DB_DSN is required",
		"SESSION_SECRET must be at least 32 characters",
		"STORAGE_ENDPOINT is required",
		"STORAGE_USE_SSL must be true/false",
		"ADMIN_SEED_USERNAME is required",
	}

	for _, part := range mustContain {
		if !strings.Contains(text, part) {
			t.Fatalf("validation error missing %q; full error: %s", part, text)
		}
	}
}

func TestLoadReadsDotEnvFile(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, ".env")
	content := strings.Join([]string{
		"APP_ENV=development",
		"PORT=8081",
		"DB_DSN=postgres://db",
		"SESSION_SECRET=12345678901234567890123456789012",
		"STORAGE_ENDPOINT=localhost:9000",
		"STORAGE_BUCKET=uploads",
		"STORAGE_ACCESS_KEY=minio",
		"STORAGE_SECRET_KEY=miniosecret",
		"ADMIN_SEED_ENABLED=false",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTP.Port != 8081 {
		t.Fatalf("port = %d, want 8081", cfg.HTTP.Port)
	}
	if cfg.AdminSeed.Enabled {
		t.Fatalf("admin seed enabled = true, want false")
	}
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
