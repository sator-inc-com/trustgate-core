package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/trustgate/trustgate/internal/config"
	"github.com/trustgate/trustgate/internal/controlplane"
)

var version = "dev"

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execute() error {
	var cfgFile string

	rootCmd := &cobra.Command{
		Use:     "aigw-server",
		Short:   "TrustGate Control Plane",
		Long:    "TrustGate Control Plane: central management for Agent fleet, policies, and audit aggregation.",
		Version: version,
	}

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the Control Plane server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadServer(cfgFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			srv, err := controlplane.New(cfg)
			if err != nil {
				return fmt.Errorf("failed to create server: %w", err)
			}

			return srv.Run()
		},
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./server.yaml)")
	rootCmd.AddCommand(serveCmd)

	return rootCmd.Execute()
}
