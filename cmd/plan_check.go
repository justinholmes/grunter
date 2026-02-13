package cmd

import (
	"fmt"
	"os"

	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/runner"
	"github.com/spf13/cobra"
)

var planCheckCmd = &cobra.Command{
	Use:   "plan-check",
	Short: "Check plan output against destroy thresholds",
	Long:  "Reads terraform/terragrunt plan output and verifies the number of resources to be destroyed does not exceed the environment's max_destroy limit.",
	RunE:  runPlanCheck,
}

func init() {
	planCheckCmd.Flags().String("env", "", "environment name to check against")
	planCheckCmd.Flags().String("input", "", "path to plan output file")
	planCheckCmd.Flags().Bool("allow-destroy", false, "override destroy protection (prints warning but exits 0)")
	_ = planCheckCmd.MarkFlagRequired("env")
	_ = planCheckCmd.MarkFlagRequired("input")
	rootCmd.AddCommand(planCheckCmd)
}

func runPlanCheck(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	envName, _ := cmd.Flags().GetString("env")
	inputPath, _ := cmd.Flags().GetString("input")
	allowDestroy, _ := cmd.Flags().GetBool("allow-destroy")

	env := cfg.GetEnvironment(envName)
	if env == nil {
		return fmt.Errorf("unknown environment %q", envName)
	}

	planOutput, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("reading plan output: %w", err)
	}

	summary := runner.ParsePlanOutput(string(planOutput))

	return checkDestroyThreshold(env, summary, allowDestroy)
}

// checkDestroyThreshold validates that the number of resources to destroy
// does not exceed the environment's max_destroy limit. Returns an error if
// the threshold is exceeded and allowDestroy is false.
func checkDestroyThreshold(env *config.Environment, summary *runner.PlanSummary, allowDestroy bool) error {
	if env.MaxDestroy == nil {
		return nil
	}

	limit := *env.MaxDestroy

	if summary.ToDestroy <= limit {
		return nil
	}

	msg := fmt.Sprintf("destroy protection: plan would destroy %d resources (limit: %d for %q)", summary.ToDestroy, limit, env.Name)

	if allowDestroy {
		fmt.Fprintf(os.Stderr, "WARNING: %s — overridden by --allow-destroy\n", msg)
		return nil
	}

	return fmt.Errorf("%s", msg)
}
