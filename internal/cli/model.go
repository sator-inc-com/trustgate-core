package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/trustgate/trustgate/internal/detector"
)

func newModelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Manage LLM detector models",
	}

	cmd.AddCommand(newModelStatusCmd())
	cmd.AddCommand(newModelDownloadCmd())
	cmd.AddCommand(newModelListCmd())

	return cmd
}

func newModelStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [model-name]",
		Short: "Check if a model is installed",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := "prompt-guard-2-86m"
			if len(args) > 0 {
				name = args[0]
			}
			fmt.Println(detector.ModelStatus(name))
		},
	}
}

func newModelDownloadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "download [model-name]",
		Short: "Download a model from HuggingFace",
		Long: `Download model files for the LLM detector.

Available models:
  prompt-guard-2-86m   Meta Prompt Guard 2 (86M) — recommended (~350MB)
  prompt-guard-2-22m   Meta Prompt Guard 2 (22M) — lightweight (~100MB)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "prompt-guard-2-86m"
			if len(args) > 0 {
				name = args[0]
			}

			info, ok := detector.AvailableModels[name]
			if !ok {
				return fmt.Errorf("unknown model: %s", name)
			}

			fmt.Printf("Downloading %s (%s)...\n", info.Name, info.Size)
			fmt.Println("  (set HF_TOKEN env var for gated models)")
			fmt.Println()
			lastFile := ""
			err := detector.DownloadModel(name, func(file string, pct int) {
				if file != lastFile {
					if lastFile != "" {
						fmt.Println() // newline after previous file
					}
					lastFile = file
				}
				bar := ""
				filled := pct / 5
				for i := 0; i < 20; i++ {
					if i < filled {
						bar += "█"
					} else {
						bar += "░"
					}
				}
				fmt.Printf("\r  %s: %s %d%%", file, bar, pct)
			})
			fmt.Println() // newline after progress bar
			if err != nil {
				return err
			}
			fmt.Println("\n✓ Download complete.")
			return nil
		},
	}
}

func newModelListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available models",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Available models:")
			fmt.Println()
			for name, info := range detector.AvailableModels {
				exists, _ := detector.ModelExists(name)
				status := "✗"
				if exists {
					status = "✓"
				}
				fmt.Printf("  %s %s (%s)\n", status, name, info.Size)
				fmt.Printf("    %s\n", info.Description)
				fmt.Printf("    %s\n\n", info.URL)
			}
		},
	}
}
