package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"gopkg.in/yaml.v3"
)

// Credentials represents saved agent registration credentials.
type Credentials struct {
	AgentID      string `yaml:"agent_id"`
	AgentToken   string `yaml:"agent_token"`
	RegisteredAt string `yaml:"registered_at"`
}

// CredentialsDir returns the platform-appropriate directory for credentials.
//   - macOS:   ~/Library/Application Support/TrustGate/
//   - Linux:   ~/.local/share/trustgate/
//   - Windows: %LOCALAPPDATA%\TrustGate\
func CredentialsDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home dir: %w", err)
		}
		return filepath.Join(home, "Library", "Application Support", "TrustGate"), nil
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("get home dir: %w", err)
			}
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(localAppData, "TrustGate"), nil
	default: // linux and others
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home dir: %w", err)
		}
		return filepath.Join(home, ".local", "share", "trustgate"), nil
	}
}

// CredentialsPath returns the full path to the credentials file.
func CredentialsPath() (string, error) {
	dir, err := CredentialsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.yaml"), nil
}

// SaveCredentials writes agent credentials to the platform-appropriate path.
func SaveCredentials(creds Credentials) error {
	path, err := CredentialsPath()
	if err != nil {
		return fmt.Errorf("get credentials path: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create credentials dir: %w", err)
	}

	if creds.RegisteredAt == "" {
		creds.RegisteredAt = time.Now().UTC().Format(time.RFC3339)
	}

	data, err := yaml.Marshal(creds)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	return nil
}

// DeleteCredentials removes the saved credentials file.
func DeleteCredentials() error {
	path, err := CredentialsPath()
	if err != nil {
		return fmt.Errorf("get credentials path: %w", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove credentials: %w", err)
	}
	return nil
}

// LoadCredentials reads agent credentials from the platform-appropriate path.
// Returns nil if the file does not exist.
func LoadCredentials() (*Credentials, error) {
	path, err := CredentialsPath()
	if err != nil {
		return nil, fmt.Errorf("get credentials path: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read credentials: %w", err)
	}

	var creds Credentials
	if err := yaml.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("unmarshal credentials: %w", err)
	}

	return &creds, nil
}
