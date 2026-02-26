package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Credentials holds a stored session token for a server.
type Credentials struct {
	Token string `json:"token"`
}

// CredentialsPath returns the path to the credentials file.
func CredentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "wasmdb", "credentials.json")
}

// LoadCredentials loads the stored token for a server URL.
func LoadCredentials(serverURL string) (*Credentials, error) {
	store, err := readCredentialsFile()
	if err != nil {
		return nil, err
	}

	cred, ok := store[serverURL]
	if !ok {
		return nil, fmt.Errorf("no credentials for %s", serverURL)
	}
	return &cred, nil
}

// SaveCredentials stores a token for a server URL.
func SaveCredentials(serverURL, token string) error {
	store, err := readCredentialsFile()
	if err != nil && !os.IsNotExist(err) {
		// If the file doesn't exist, start fresh.
		store = make(map[string]Credentials)
	}
	if store == nil {
		store = make(map[string]Credentials)
	}

	store[serverURL] = Credentials{Token: token}
	return writeCredentialsFile(store)
}

// DeleteCredentials removes stored credentials for a server URL.
func DeleteCredentials(serverURL string) error {
	store, err := readCredentialsFile()
	if err != nil {
		return nil
	}

	delete(store, serverURL)
	return writeCredentialsFile(store)
}

func readCredentialsFile() (map[string]Credentials, error) {
	data, err := os.ReadFile(CredentialsPath())
	if err != nil {
		return nil, err
	}

	var store map[string]Credentials
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	return store, nil
}

func writeCredentialsFile(store map[string]Credentials) error {
	path := CredentialsPath()
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}
