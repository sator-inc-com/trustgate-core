package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadServer reads the Control Plane configuration from a YAML file.
func LoadServer(path string) (*ServerConfig, error) {
	if path == "" {
		path = "server.yaml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return serverDefaults(), nil
		}
		return nil, fmt.Errorf("read server config file: %w", err)
	}

	cfg := serverDefaults()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse server config file: %w", err)
	}

	return cfg, nil
}

func serverDefaults() *ServerConfig {
	return &ServerConfig{
		Version: "1",
		Listen: ServerListenConfig{
			Host: "0.0.0.0",
			Port: 9090,
		},
		Database: DatabaseConfig{
			Path: "./controlplane.db",
		},
		Auth: AuthConfig{},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
	}
}
