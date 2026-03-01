package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds persistent CLI configuration.
type Config struct {
	// URL is the default server URL (overridden by --url flag or WASMDB_URL env).
	URL string `json:"url,omitempty"`

	// DefaultFormat is the default output format ("text" or "json").
	DefaultFormat string `json:"default_format,omitempty"`
}

// ConfigPath returns the path to the config file.
func ConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "wasmdb", "config.json")
}

// LoadConfig loads the CLI config file.
// Returns a zero-value Config (not an error) if the file doesn't exist.
func LoadConfig() (*Config, error) {
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveConfig writes the CLI config file.
func SaveConfig(cfg *Config) error {
	path := ConfigPath()
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}
