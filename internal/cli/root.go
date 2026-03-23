package cli

import (
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	verbose bool
)

func Execute(version string) error {
	rootCmd := &cobra.Command{
		Use:     "aigw",
		Short:   "TrustGate - AI Zero Trust Gateway",
		Long:    "TrustGate Agent: inspects and controls AI input/output in real-time.",
		Version: version,
	}

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./agent.yaml)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "verbose output")

	rootCmd.AddCommand(
		newServeCmd(),
		newServiceCmd(),
		newInitCmd(),
		newTestCmd(),
		newLogsCmd(),
		newDoctorCmd(),
		newModelCmd(),
	)

	return rootCmd.Execute()
}
