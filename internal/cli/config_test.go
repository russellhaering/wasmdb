package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	// Use a temp dir as home.
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	// LoadConfig should return empty config when no file exists.
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig on missing file: %v", err)
	}
	if cfg.URL != "" {
		t.Errorf("expected empty URL, got %q", cfg.URL)
	}

	// SaveConfig should create the file.
	cfg.URL = "https://example.com"
	cfg.DefaultFormat = "json"
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Verify file exists.
	path := filepath.Join(tmpDir, ".config", "wasmdb", "config.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not created: %v", err)
	}

	// Reload and verify.
	cfg2, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig after save: %v", err)
	}
	if cfg2.URL != "https://example.com" {
		t.Errorf("URL = %q, want %q", cfg2.URL, "https://example.com")
	}
	if cfg2.DefaultFormat != "json" {
		t.Errorf("DefaultFormat = %q, want %q", cfg2.DefaultFormat, "json")
	}
}
