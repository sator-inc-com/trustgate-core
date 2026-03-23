package cli

import (
	"fmt"
	"os"

	"github.com/kardianos/service"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/trustgate/trustgate/internal/config"
	"github.com/trustgate/trustgate/internal/gateway"
	"github.com/trustgate/trustgate/internal/policy"
	syncpkg "github.com/trustgate/trustgate/internal/sync"
	"gopkg.in/yaml.v3"
)

// svcWrapper wraps the gateway server for kardianos/service.
type svcWrapper struct {
	srv *gateway.Server
}

func (w *svcWrapper) Start(s service.Service) error {
	go w.srv.Run()
	return nil
}

func (w *svcWrapper) Stop(s service.Service) error {
	return nil
}

func newServeCmd() *cobra.Command {
	var host string
	var port int
	var llmDetector bool
	var mockBackend bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start TrustGate Agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if host != "" {
				cfg.Listen.Host = host
			}
			if port != 0 {
				cfg.Listen.Port = port
			}
			if llmDetector {
				cfg.Detectors.LLM.Enabled = true
			}
			if mockBackend {
				cfg.Backend.Provider = "mock"
			}

			srv, err := gateway.New(cfg)
			if err != nil {
				return fmt.Errorf("failed to create server: %w", err)
			}

			// Start sync client in managed mode
			if cfg.Mode == "managed" && cfg.Sync.ServerURL != "" {
				logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
				syncClient := syncpkg.NewClient(cfg.Sync, logger)

				// Wire policy pull to the evaluator's hot reload
				evaluator := srv.Evaluator()
				syncClient.SetPolicyUpdateCallback(func(version int, policyContents []string) {
					var policies []policy.Policy
					for _, content := range policyContents {
						// Each content is a YAML policy document (single policy)
						var p policy.Policy
						if err := yaml.Unmarshal([]byte(content), &p); err != nil {
							logger.Error().Err(err).Str("content", content[:min(len(content), 80)]).Msg("failed to parse policy from CP")
							continue
						}
						if p.Mode == "" {
							p.Mode = "enforce"
						}
						policies = append(policies, p)
					}
					evaluator.UpdatePolicies(policies)
					logger.Info().Int("version", version).Int("policies", len(policies)).Msg("policies hot-reloaded from control plane")
				})

				// Wire stats recording: gateway → sync client → CP
				srv.SetStatsRecorder(syncClient)

				// Wire audit WAL flush: gateway WAL → sync client → CP
				if wal := srv.AuditWAL(); wal != nil {
					syncClient.SetAuditWAL(wal)
				}

				if err := syncClient.Start(); err != nil {
					logger.Warn().Err(err).Msg("sync client failed to start, continuing in standalone mode")
				} else {
					defer syncClient.Stop()
				}
				fmt.Printf("  Sync:       managed mode (%s)\n", cfg.Sync.ServerURL)
			}

			return srv.Run()
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "listen address (overrides config)")
	cmd.Flags().IntVar(&port, "port", 0, "listen port (overrides config)")
	cmd.Flags().BoolVar(&llmDetector, "llm-detector", false, "enable Prompt Guard 2 LLM detector (Stage 2)")
	cmd.Flags().BoolVar(&mockBackend, "mock-backend", false, "use mock backend (no AWS credentials needed)")

	return cmd
}

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service [install|uninstall|start|stop|status]",
		Short: "Manage TrustGate as a system service",
		Long: `Install and manage TrustGate Agent as a system service.

Windows: Runs as a Windows Service
Linux:   Runs as a systemd service
macOS:   Runs as a launchd service`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svcConfig := &service.Config{
				Name:        "TrustGate",
				DisplayName: "TrustGate Agent",
				Description: "AI Zero Trust Gateway - inspects and controls AI input/output",
			}

			// Add config flag as service argument if specified
			if cfgFile != "" {
				svcConfig.Arguments = []string{"serve", "--config", cfgFile}
			} else {
				svcConfig.Arguments = []string{"serve"}
			}

			wrapper := &svcWrapper{}
			svc, err := service.New(wrapper, svcConfig)
			if err != nil {
				return fmt.Errorf("create service: %w", err)
			}

			action := args[0]
			switch action {
			case "install":
				err = svc.Install()
				if err != nil {
					return fmt.Errorf("install service: %w", err)
				}
				fmt.Println("Service installed successfully.")
				fmt.Println("  Start: aigw service start")
				fmt.Println("  Stop:  aigw service stop")
				return nil

			case "uninstall":
				err = svc.Uninstall()
				if err != nil {
					return fmt.Errorf("uninstall service: %w", err)
				}
				fmt.Println("Service uninstalled.")
				return nil

			case "start":
				err = svc.Start()
				if err != nil {
					return fmt.Errorf("start service: %w", err)
				}
				fmt.Println("Service started.")
				return nil

			case "stop":
				err = svc.Stop()
				if err != nil {
					return fmt.Errorf("stop service: %w", err)
				}
				fmt.Println("Service stopped.")
				return nil

			case "status":
				status, err := svc.Status()
				if err != nil {
					return fmt.Errorf("get status: %w", err)
				}
				switch status {
				case service.StatusRunning:
					fmt.Println("Status: running")
				case service.StatusStopped:
					fmt.Println("Status: stopped")
				default:
					fmt.Println("Status: unknown")
				}
				return nil

			default:
				return fmt.Errorf("unknown action %q (use install|uninstall|start|stop|status)", action)
			}
		},
	}

	return cmd
}
