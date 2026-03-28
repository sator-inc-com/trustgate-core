package cli

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/trustgate/trustgate/internal/config"
	"github.com/trustgate/trustgate/internal/detector"
	"github.com/trustgate/trustgate/internal/policy"
)

type checkResult struct {
	status  string // "pass", "fail", "warn"
	label   string
	detail  string
	hint    string
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose environment and config",
		Long:  `Run environment diagnostic checks to verify the TrustGate Agent setup.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("\n%sTrustGate Environment Check%s\n\n", colorBold, colorReset)

			var results []checkResult

			// 1. Go runtime
			results = append(results, checkResult{
				status: "pass",
				label:  "Go runtime",
				detail: runtime.Version(),
			})

			// 2. Config file
			configPath := cfgFile
			if configPath == "" {
				configPath = "agent.yaml"
			}
			cfg, cfgErr := config.Load(configPath)
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				results = append(results, checkResult{
					status: "fail",
					label:  "Config",
					detail: configPath + " not found",
					hint:   "Run: aigw init",
				})
			} else if cfgErr != nil {
				results = append(results, checkResult{
					status: "fail",
					label:  "Config",
					detail: fmt.Sprintf("%s invalid: %v", configPath, cfgErr),
					hint:   "Fix YAML syntax errors in " + configPath,
				})
			} else {
				results = append(results, checkResult{
					status: "pass",
					label:  "Config",
					detail: configPath + " found",
				})
			}

			// Use defaults if config failed to load
			if cfg == nil {
				cfg = &config.Config{
					Listen: config.ListenConfig{Port: 8787},
					Policy: config.PolicyConfig{File: "policies.yaml"},
					Audit:  config.AuditConfig{Path: "audit.db"},
				}
			}

			// 3. Policies file
			policiesPath := cfg.Policy.File
			if policiesPath == "" {
				policiesPath = "policies.yaml"
			}
			policies, polErr := policy.LoadPolicies(policiesPath)
			if os.IsNotExist(polErr) || (polErr != nil && isNotExist(polErr)) {
				results = append(results, checkResult{
					status: "fail",
					label:  "Policies",
					detail: policiesPath + " not found",
					hint:   "Run: aigw init",
				})
			} else if polErr != nil {
				results = append(results, checkResult{
					status: "fail",
					label:  "Policies",
					detail: fmt.Sprintf("%s invalid: %v", policiesPath, polErr),
				})
			} else {
				results = append(results, checkResult{
					status: "pass",
					label:  "Policies",
					detail: fmt.Sprintf("%s found (%d policies loaded)", policiesPath, len(policies)),
				})
			}

			// 4. Port availability
			portStr := strconv.Itoa(cfg.Listen.Port)
			listener, err := net.Listen("tcp", cfg.Listen.Host+":"+portStr)
			if err != nil {
				results = append(results, checkResult{
					status: "fail",
					label:  "Port " + portStr,
					detail: "in use or unavailable",
					hint:   "Change listen.port in agent.yaml or stop the process using port " + portStr,
				})
			} else {
				listener.Close()
				results = append(results, checkResult{
					status: "pass",
					label:  "Port " + portStr,
					detail: "available",
				})
			}

			// 5. Audit DB path writable
			auditPath := cfg.Audit.Path
			if auditPath == "" {
				auditPath = "audit.db"
			}
			auditDir := filepath.Dir(auditPath)
			if auditDir == "" || auditDir == "." {
				auditDir, _ = os.Getwd()
			}
			if isWritable(auditDir) {
				results = append(results, checkResult{
					status: "pass",
					label:  "Audit DB",
					detail: fmt.Sprintf("%s (writable)", auditPath),
				})
			} else {
				results = append(results, checkResult{
					status: "fail",
					label:  "Audit DB",
					detail: fmt.Sprintf("%s (not writable)", auditPath),
					hint:   "Ensure the directory is writable: " + auditDir,
				})
			}

			// 6. Detectors
			detectorCount := 0
			var detectorNames []string
			if cfg.Detectors.PII.Enabled {
				detectorCount++
				detectorNames = append(detectorNames, "pii")
			}
			if cfg.Detectors.Injection.Enabled {
				detectorCount++
				detectorNames = append(detectorNames, "injection")
			}
			if cfg.Detectors.Confidential.Enabled {
				detectorCount++
				detectorNames = append(detectorNames, "confidential")
			}
			if detectorCount > 0 {
				results = append(results, checkResult{
					status: "pass",
					label:  "Detectors",
					detail: fmt.Sprintf("%d loaded (%s)", detectorCount, joinStrings(detectorNames)),
				})
			} else {
				results = append(results, checkResult{
					status: "warn",
					label:  "Detectors",
					detail: "no detectors enabled",
					hint:   "Enable detectors in agent.yaml (pii, injection, confidential)",
				})
			}

			// 7. LLM Model
			modelName := cfg.Detectors.LLM.Model
			if modelName == "" {
				modelName = "prompt-guard-2-22m"
			}
			modelExists, _ := detector.ModelExists(modelName)
			if modelExists {
				results = append(results, checkResult{
					status: "pass",
					label:  "LLM Model",
					detail: modelName + " installed",
				})
			} else {
				results = append(results, checkResult{
					status: "warn",
					label:  "LLM Model",
					detail: modelName + " not installed (optional, Stage 2 detection disabled)",
					hint:   "Run: aigw model download " + modelName,
				})
			}

			// 8. Content inspection
			if cfg.ContentInspection.Enabled {
				results = append(results, checkResult{
					status: "pass",
					label:  "Content inspection",
					detail: "enabled",
				})
			} else {
				results = append(results, checkResult{
					status: "pass",
					label:  "Content inspection",
					detail: "disabled (enable in agent.yaml if needed)",
				})
			}

			// 9. ONNX Runtime
			onnxFound := false
			onnxPaths := onnxRuntimeSearchPaths()
			for _, p := range onnxPaths {
				if _, err := os.Stat(p); err == nil {
					onnxFound = true
					results = append(results, checkResult{
						status: "pass",
						label:  "ONNX Runtime",
						detail: fmt.Sprintf("found at %s", p),
					})
					break
				}
			}
			if !onnxFound {
				results = append(results, checkResult{
					status: "warn",
					label:  "ONNX Runtime",
					detail: "not found (required for Stage 2 LLM detector)",
					hint:   "Install ONNX Runtime: https://onnxruntime.ai/docs/install/",
				})
			}

			// 10. Sync mode
			if cfg.Mode == "managed" && cfg.Sync.ServerURL != "" {
				results = append(results, checkResult{
					status: "pass",
					label:  "Sync",
					detail: fmt.Sprintf("managed mode (%s)", cfg.Sync.ServerURL),
				})
			} else {
				results = append(results, checkResult{
					status: "warn",
					label:  "Sync",
					detail: "standalone mode (no Control Plane configured)",
				})
			}

			// Print results
			passed := 0
			failed := 0
			warnings := 0
			for _, r := range results {
				switch r.status {
				case "pass":
					passed++
					fmt.Printf("%s %s: %s\n", checkMark, r.label, r.detail)
				case "fail":
					failed++
					fmt.Printf("%s %s: %s\n", crossMark, r.label, r.detail)
					if r.hint != "" {
						fmt.Printf("  %s\u2192 %s%s\n", colorYellow, r.hint, colorReset)
					}
				case "warn":
					warnings++
					fmt.Printf("%s  %s: %s\n", warningMark, r.label, r.detail)
					if r.hint != "" {
						fmt.Printf("  %s\u2192 %s%s\n", colorYellow, r.hint, colorReset)
					}
				}
			}

			fmt.Printf("\n%sSummary:%s %d passed, %d failed, %d warning\n\n",
				colorBold, colorReset, passed, failed, warnings)

			if failed > 0 {
				return fmt.Errorf("%d check(s) failed", failed)
			}
			return nil
		},
	}
}

// isWritable checks if a directory is writable by creating a temp file.
func isWritable(dir string) bool {
	tmp, err := os.CreateTemp(dir, ".aigw-doctor-*")
	if err != nil {
		return false
	}
	tmp.Close()
	os.Remove(tmp.Name())
	return true
}

// isNotExist checks if an error wraps a "not exist" error.
func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	return os.IsNotExist(err) || containsNotExist(err.Error())
}

func containsNotExist(s string) bool {
	return len(s) > 0 && (contains(s, "no such file") || contains(s, "not exist"))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func joinStrings(strs []string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}

// onnxRuntimeSearchPaths returns candidate paths for ONNX Runtime library.
func onnxRuntimeSearchPaths() []string {
	var libName string
	switch runtime.GOOS {
	case "windows":
		libName = "onnxruntime.dll"
	case "darwin":
		libName = "libonnxruntime.dylib"
	default:
		libName = "libonnxruntime.so"
	}

	var paths []string

	// Same directory as executable
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), libName))
	}

	// Current working directory
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, libName))
	}

	// Platform-specific
	switch runtime.GOOS {
	case "windows":
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			paths = append(paths, filepath.Join(pf, "TrustGate", libName))
		}
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			paths = append(paths, filepath.Join(la, "TrustGate", libName))
		}
	case "darwin":
		paths = append(paths,
			filepath.Join("/usr/local/lib", libName),
			filepath.Join("/opt/homebrew/lib", libName),
		)
		if home, err := os.UserHomeDir(); err == nil {
			paths = append(paths, filepath.Join(home, "Library", "Application Support", "TrustGate", libName))
		}
	default:
		paths = append(paths,
			filepath.Join("/usr/lib", libName),
			filepath.Join("/usr/local/lib", libName),
		)
	}

	return paths
}
