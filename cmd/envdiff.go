package cmd

import (
	"fmt"
	"os"

	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/envs"
	"github.com/spf13/cobra"
)

var envdiffCmd = &cobra.Command{
	Use:   "envdiff <source> <target>",
	Short: "Compare two environments",
	Long:  "Shows structural differences (units in one but not the other) and content diffs (line-by-line terragrunt.hcl differences) between two environments.",
	Args:  cobra.ExactArgs(2),
	RunE:  runEnvDiff,
}

func init() {
	envdiffCmd.Flags().Bool("json", false, "output as JSON")
	envdiffCmd.Flags().StringP("output", "o", "", "write report to file instead of stdout")
	rootCmd.AddCommand(envdiffCmd)
}

func runEnvDiff(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	sourceName, targetName := args[0], args[1]

	source := cfg.GetEnvironment(sourceName)
	if source == nil {
		return fmt.Errorf("unknown environment %q", sourceName)
	}
	target := cfg.GetEnvironment(targetName)
	if target == nil {
		return fmt.Errorf("unknown environment %q", targetName)
	}

	result, err := envs.Diff(*source, *target)
	if err != nil {
		return fmt.Errorf("diffing environments: %w", err)
	}

	useJSON, _ := cmd.Flags().GetBool("json")
	var output string
	if useJSON {
		output, err = envs.FormatDiffJSON(result)
		if err != nil {
			return fmt.Errorf("formatting JSON: %w", err)
		}
	} else {
		output = envs.FormatDiffMarkdown(result)
	}

	outPath, _ := cmd.Flags().GetString("output")
	if outPath != "" {
		return os.WriteFile(outPath, []byte(output), 0644)
	}

	fmt.Print(output)
	return nil
}
