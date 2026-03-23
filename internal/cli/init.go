package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorBold   = "\033[1m"
	colorCyan   = "\033[36m"

	checkMark   = colorGreen + "\u2713" + colorReset
	crossMark   = colorRed + "\u2717" + colorReset
	warningMark = colorYellow + "\u26A0" + colorReset
)

func newInitCmd() *cobra.Command {
	var provider string
	var port int
	var withSamples bool
	var force bool
	var mode string
	var serverURL string
	var apiKey string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate initial config files",
		Long: `Interactive setup wizard that generates agent.yaml and policies.yaml.

Use flags for non-interactive mode:
  aigw init --provider bedrock --port 8787

For managed mode (connected to Control Plane):
  aigw init --mode managed --server http://cp:9090 --api-key tg_dept_sales_abc123`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("\n%sTrustGate Agent Setup%s\n\n", colorBold, colorReset)

			scanner := bufio.NewScanner(os.Stdin)

			// Managed mode: if all three flags are provided, generate managed config non-interactively
			if mode == "managed" && serverURL != "" && apiKey != "" {
				agentPath := "agent.yaml"
				policiesPath := "policies.yaml"
				if !force {
					if fileExists(agentPath) {
						return fmt.Errorf("agent.yaml already exists (use --force to overwrite)")
					}
				}

				agentYAML := generateManagedAgentYAML(serverURL, apiKey, port)
				if err := os.WriteFile(agentPath, []byte(agentYAML), 0644); err != nil {
					return fmt.Errorf("write agent.yaml: %w", err)
				}
				fmt.Printf("%s Generated agent.yaml (managed mode)\n", checkMark)

				if !fileExists(policiesPath) || force {
					policiesYAML := generatePoliciesYAML(withSamples)
					if err := os.WriteFile(policiesPath, []byte(policiesYAML), 0644); err != nil {
						return fmt.Errorf("write policies.yaml: %w", err)
					}
					fmt.Printf("%s Generated policies.yaml\n", checkMark)
				}

				fmt.Printf("\n%sNext steps:%s\n", colorBold, colorReset)
				fmt.Println("  1. aigw doctor     \u2014 check your environment")
				fmt.Println("  2. aigw serve      \u2014 start the agent (will auto-register with Control Plane)")
				fmt.Println()
				return nil
			}

			// Determine if non-interactive (all key flags provided)
			interactive := provider == "" || port == 0

			// Provider
			if provider == "" {
				provider = promptWithDefault(scanner, "Provider", "bedrock")
			}

			// Port
			if port == 0 {
				portStr := promptWithDefault(scanner, "Listen port", "8787")
				var err error
				port, err = strconv.Atoi(portStr)
				if err != nil {
					port = 8787
				}
			}

			// Detection settings (only in interactive mode)
			enablePII := true
			enableInjection := true
			enableConfidential := true
			languages := "en,ja"

			if interactive {
				enablePII = promptYesNo(scanner, "Enable PII detection?", true)
				enableInjection = promptYesNo(scanner, "Enable injection detection?", true)
				enableConfidential = promptYesNo(scanner, "Enable confidential detection?", true)
				languages = promptWithDefault(scanner, "Detection languages", "en,ja")
			}

			fmt.Println()

			// Check for existing files
			agentPath := "agent.yaml"
			policiesPath := "policies.yaml"

			if !force {
				if fileExists(agentPath) {
					if interactive {
						if !promptYesNo(scanner, "agent.yaml already exists. Overwrite?", false) {
							fmt.Println("Aborted.")
							return nil
						}
					} else {
						return fmt.Errorf("agent.yaml already exists (use --force to overwrite)")
					}
				}
				if fileExists(policiesPath) {
					if interactive {
						if !promptYesNo(scanner, "policies.yaml already exists. Overwrite?", false) {
							fmt.Println("Aborted.")
							return nil
						}
					} else {
						return fmt.Errorf("policies.yaml already exists (use --force to overwrite)")
					}
				}
			}

			// Generate agent.yaml
			langList := strings.Split(languages, ",")
			for i := range langList {
				langList[i] = strings.TrimSpace(langList[i])
			}

			agentYAML := generateAgentYAML(provider, port, enablePII, enableInjection, enableConfidential, langList)
			if err := os.WriteFile(agentPath, []byte(agentYAML), 0644); err != nil {
				return fmt.Errorf("write agent.yaml: %w", err)
			}
			fmt.Printf("%s Generated agent.yaml\n", checkMark)

			// Generate policies.yaml
			policiesYAML := generatePoliciesYAML(withSamples)
			if err := os.WriteFile(policiesPath, []byte(policiesYAML), 0644); err != nil {
				return fmt.Errorf("write policies.yaml: %w", err)
			}
			if withSamples {
				fmt.Printf("%s Generated policies.yaml (with sample policies and comments)\n", checkMark)
			} else {
				fmt.Printf("%s Generated policies.yaml (default policies)\n", checkMark)
			}

			fmt.Printf("\n%sNext steps:%s\n", colorBold, colorReset)
			fmt.Println("  1. aigw doctor     \u2014 check your environment")
			fmt.Println("  2. aigw serve      \u2014 start the agent")
			fmt.Println("  3. aigw test       \u2014 run policy tests")
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "", "LLM backend provider (default: bedrock)")
	cmd.Flags().IntVar(&port, "port", 0, "listen port (default: 8787)")
	cmd.Flags().BoolVar(&withSamples, "with-samples", false, "include sample policies with comments")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files without asking")
	cmd.Flags().StringVar(&mode, "mode", "standalone", "agent mode: standalone or managed")
	cmd.Flags().StringVar(&serverURL, "server", "", "Control Plane URL (for managed mode)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key for Control Plane (org or department key)")

	return cmd
}

func promptWithDefault(scanner *bufio.Scanner, label, defaultVal string) string {
	fmt.Printf("%s? %s%s [%s] (default: %s): ", colorCyan, label, colorReset, defaultVal, defaultVal)
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			return input
		}
	}
	return defaultVal
}

func promptYesNo(scanner *bufio.Scanner, label string, defaultYes bool) bool {
	hint := "Y/n"
	if !defaultYes {
		hint = "y/N"
	}
	fmt.Printf("%s? %s%s [%s]: ", colorCyan, label, colorReset, hint)
	if scanner.Scan() {
		input := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if input == "" {
			return defaultYes
		}
		return input == "y" || input == "yes"
	}
	return defaultYes
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func generateAgentYAML(provider string, port int, enablePII, enableInjection, enableConfidential bool, languages []string) string {
	langYAML := "[" + strings.Join(languages, ", ") + "]"

	return fmt.Sprintf(`version: "1"
mode: standalone

listen:
  host: 127.0.0.1
  port: %d
  allow_debug: true

identity:
  mode: header
  headers:
    user_id: X-TrustGate-User
    role: X-TrustGate-Role
    department: X-TrustGate-Department
    clearance: X-TrustGate-Clearance
  on_missing: anonymous
  anonymous_role: guest

detectors:
  pii:
    enabled: %t
    patterns:
      email: true
      phone: true
      mobile: true
      my_number: true
      credit_card: true
  injection:
    enabled: %t
    language: %s
  confidential:
    enabled: %t
    keywords:
      critical: ["\u6975\u79d8", "\u793e\u5916\u79d8", "CONFIDENTIAL", "TOP SECRET"]
      high: ["\u6a5f\u5bc6", "\u5185\u90e8\u9650\u5b9a", "INTERNAL ONLY"]

policy:
  source: local
  file: policies.yaml

audit:
  mode: memory
  max_entries: 100

logging:
  level: info
  format: text

backend:
  provider: %s
  bedrock:
    region: ap-northeast-1
  mock:
    delay_ms: 50
`, port, enablePII, enableInjection, langYAML, enableConfidential, provider)
}

func generateManagedAgentYAML(serverURL, apiKey string, port int) string {
	if port == 0 {
		port = 8787
	}
	return fmt.Sprintf(`version: "1"
mode: managed

sync:
  server_url: %s
  api_key: "%s"
  labels:
    environment: "production"
  heartbeat_sec: 30
  stats_push_sec: 60

listen:
  host: 127.0.0.1
  port: %d

detectors:
  pii:
    enabled: true
  injection:
    enabled: true
    language: [en, ja]
  confidential:
    enabled: true
    keywords:
      critical: ["\u6975\u79d8", "\u793e\u5916\u79d8", "CONFIDENTIAL", "TOP SECRET"]
      high: ["\u6a5f\u5bc6", "\u5185\u90e8\u9650\u5b9a", "INTERNAL ONLY"]

policy:
  source: local
  file: policies.yaml

audit:
  mode: local
  path: audit.db
  retention_days: 90
  max_size_mb: 500

logging:
  level: info
  format: text
`, serverURL, apiKey, port)
}

func generatePoliciesYAML(withSamples bool) string {
	if withSamples {
		return `version: "1"

policies:
  # ============================================================
  # Prompt Injection Detection
  # ============================================================
  # Blocks prompt injection attacks that try to override system instructions.
  # Covers both English and Japanese attack patterns.
  # Whitelisted for admin/security roles who may need to test.

  # Block critical injection attacks on input
  - name: block_injection_critical
    phase: input
    when:
      detector: injection
      min_severity: critical
    action: block
    message: "Prompt injection detected. This attempt has been logged."
    whitelist:
      identity:
        role: [admin, security]

  # Warn on high-severity injection attempts on input
  - name: warn_injection_high
    phase: input
    when:
      detector: injection
      min_severity: high
    action: warn
    whitelist:
      identity:
        role: [admin, security]

  # Block critical injection in AI responses (indirect injection via RAG)
  - name: block_injection_output_critical
    phase: output
    when:
      detector: injection
      min_severity: critical
    action: block
    message: "Malicious instructions detected in AI response. This has been logged."
    whitelist:
      identity:
        role: [admin, security]

  # ============================================================
  # PII (Personal Information) Detection
  # ============================================================
  # Blocks personal information from being sent to AI models.
  # Detects: email, phone, credit card, My Number, etc.

  # Block PII in user input
  - name: block_pii_input
    phase: input
    when:
      detector: pii
      min_severity: high
    action: block
    message: "Personal information detected. Please remove PII before sending."
    whitelist:
      identity:
        role: [admin, security]

  # Warn on PII in AI responses
  - name: warn_pii_output
    phase: output
    when:
      detector: pii
      min_severity: high
    action: warn
    whitelist:
      identity:
        role: [admin, security]

  # ============================================================
  # Confidential Information Detection (Shadow Mode)
  # ============================================================
  # Shadow mode: logs what would be blocked without actually blocking.
  # Use this to measure false positive rates before enforcing.
  # Change mode from "shadow" to "enforce" when ready.

  # Shadow-block confidential info in input
  - name: block_confidential_input
    phase: input
    when:
      detector: confidential
      min_severity: critical
    action: block
    mode: shadow
    message: "Confidential information detected."
    whitelist:
      identity:
        role: [admin, security]

  # Shadow-block confidential info in output
  - name: block_confidential_output
    phase: output
    when:
      detector: confidential
      min_severity: critical
    action: block
    mode: shadow
    message: "Confidential information detected in AI response."
    whitelist:
      identity:
        role: [admin, security]
`
	}

	return `version: "1"

policies:
  - name: block_injection_critical
    phase: input
    when:
      detector: injection
      min_severity: critical
    action: block
    message: "Prompt injection detected. This attempt has been logged."
    whitelist:
      identity:
        role: [admin, security]

  - name: warn_injection_high
    phase: input
    when:
      detector: injection
      min_severity: high
    action: warn
    whitelist:
      identity:
        role: [admin, security]

  - name: block_injection_output_critical
    phase: output
    when:
      detector: injection
      min_severity: critical
    action: block
    message: "Malicious instructions detected in AI response."
    whitelist:
      identity:
        role: [admin, security]

  - name: warn_injection_output_high
    phase: output
    when:
      detector: injection
      min_severity: high
    action: warn
    whitelist:
      identity:
        role: [admin, security]

  - name: block_pii_input
    phase: input
    when:
      detector: pii
      min_severity: high
    action: block
    message: "Personal information detected. Please remove PII before sending."
    whitelist:
      identity:
        role: [admin, security]

  - name: warn_pii_output
    phase: output
    when:
      detector: pii
      min_severity: high
    action: warn
    whitelist:
      identity:
        role: [admin, security]

  - name: block_confidential_input
    phase: input
    when:
      detector: confidential
      min_severity: critical
    action: block
    mode: shadow
    message: "Confidential information detected."
    whitelist:
      identity:
        role: [admin, security]

  - name: block_confidential_output
    phase: output
    when:
      detector: confidential
      min_severity: critical
    action: block
    mode: shadow
    message: "Confidential information detected in AI response."
    whitelist:
      identity:
        role: [admin, security]
`
}
