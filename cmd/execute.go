package cmd

import (
	"fmt"
	"os"

	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/runner"
	"github.com/spf13/cobra"
)

var executeCmd = &cobra.Command{
	Use:   "execute [plan|apply] <unit-path>",
	Short: "Run terragrunt plan or apply on a single unit",
	Long:  "Executes terragrunt plan or apply in the specified unit directory, capturing output for later use.",
	Args:  cobra.ExactArgs(2),
	RunE:  runExecute,
}

func init() {
	executeCmd.Flags().StringP("output", "o", "", "write output to file")
	executeCmd.Flags().Duration("timeout", 0, "command timeout (e.g. 30m)")
	rootCmd.AddCommand(executeCmd)
}

func runExecute(cmd *cobra.Command, args []string) error {
	action := args[0]
	unitPath := args[1]

	if action != "plan" && action != "apply" {
		return fmt.Errorf("action must be 'plan' or 'apply', got %q", action)
	}

	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	timeout, _ := cmd.Flags().GetDuration("timeout")

	r := runner.New(cfg.TFBinary, cfg.TGBinary)
	result, err := r.Run(unitPath, action, timeout)
	if err != nil {
		return fmt.Errorf("running %s on %s: %w", action, unitPath, err)
	}

	combined := result.CombinedOutput()

	outPath, _ := cmd.Flags().GetString("output")
	if outPath != "" {
		if writeErr := os.WriteFile(outPath, []byte(combined), 0644); writeErr != nil {
			return fmt.Errorf("writing output: %w", writeErr)
		}
	} else {
		fmt.Print(combined)
	}

	if !result.Success {
		os.Exit(1)
	}

	return nil
}
