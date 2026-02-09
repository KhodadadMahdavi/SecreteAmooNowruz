package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDotEnvFile(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, ".env")
	content := `
# comment line
export FOO=bar
EMPTY=
QUOTED="value with spaces"
SINGLE='single quoted'
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	values, err := ParseDotEnvFile(path)
	if err != nil {
		t.Fatalf("ParseDotEnvFile() error = %v", err)
	}

	if values["FOO"] != "bar" {
		t.Fatalf("FOO = %q, want bar", values["FOO"])
	}
	if values["EMPTY"] != "" {
		t.Fatalf("EMPTY = %q, want empty string", values["EMPTY"])
	}
	if values["QUOTED"] != "value with spaces" {
		t.Fatalf("QUOTED = %q, want value with spaces", values["QUOTED"])
	}
	if values["SINGLE"] != "single quoted" {
		t.Fatalf("SINGLE = %q, want single quoted", values["SINGLE"])
	}
}

func TestParseDotEnvFileInvalidLine(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, ".env")
	content := `
INVALID_LINE
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ParseDotEnvFile(path)
	if err == nil {
		t.Fatal("ParseDotEnvFile() error = nil, want invalid line error")
	}
}
