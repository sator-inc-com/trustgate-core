package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

func Load(path string) (*Config, error) {
	if path == "" {
		path = "agent.yaml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaults(), nil
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := defaults()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	return cfg, nil
}

func defaults() *Config {
	return &Config{
		Version: "1",
		Mode:    "standalone",
		Listen: ListenConfig{
			Host:             "127.0.0.1",
			Port:             8787,
			ReadTimeout:      30 * time.Second,
			WriteTimeout:     120 * time.Second,
			IncludeTrustgate: true,
			AllowDebug:       true,
		},
		Identity: IdentityConfig{
			Mode: "header",
			Headers: map[string]string{
				"user_id":    "X-TrustGate-User",
				"role":       "X-TrustGate-Role",
				"department": "X-TrustGate-Department",
				"clearance":  "X-TrustGate-Clearance",
			},
			OnMissing:     "anonymous",
			AnonymousRole: "guest",
		},
		Workforce: WorkforceConfig{
			Enabled: false,
			TargetSites: TargetSitesConfig{
				Include: []string{
					"https://chatgpt.com/*",
					"https://chat.openai.com/*",
					"https://gemini.google.com/*",
					"https://claude.ai/*",
					"https://copilot.microsoft.com/*",
				},
			},
		},
		Detectors: DetectorConfig{
			PII: PIIConfig{
				Enabled: true,
				Patterns: map[string]bool{
					"email":       true,
					"phone":       true,
					"mobile":      true,
					"my_number":   true,
					"credit_card": true,
				},
			},
			Injection: InjectionConfig{
				Enabled:  true,
				Language: []string{"en", "ja"},
			},
			Confidential: ConfidentialConfig{
				Enabled: true,
				Keywords: map[string][]string{
					"critical": {"極秘", "社外秘", "CONFIDENTIAL", "TOP SECRET"},
					"high":     {"機密", "内部限定", "INTERNAL ONLY"},
				},
			},
			ToolInspection: ToolInspectionConfig{
				Enabled:            true,
				InspectArguments:   true,
				InspectToolResults: true,
			},
		},
		ResponseInspection: ResponseInspectionConfig{
			Mode:             "windowed",
			WindowSize:       1024,
			Overlap:          64,
			MaxBuffer:        1048576,
			OnBufferOverflow: "allow",
		},
		Policy: PolicyConfig{
			Source: "local",
			File:   "policies.yaml",
		},
		Audit: AuditConfig{
			Mode:          "local",
			Path:          "audit.db",
			RetentionDays: 90,
			MaxSizeMB:     500,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		Backend: BackendConfig{
			Provider: "mock",
			Bedrock: BedrockConfig{
				Region: "ap-northeast-1",
			},
			Mock: MockConfig{
				DelayMs: 50,
			},
		},
	}
}
