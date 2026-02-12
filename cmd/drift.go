package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/plan"
	"github.com/justinholmes/grunter/internal/runner"
	"github.com/spf13/cobra"
)

var driftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Detect drift across all Terragrunt units",
	Long:  "Discovers all Terragrunt units in the repo and runs plan on each to identify drift.",
	RunE:  runDrift,
}

func init() {
	driftCmd.Flags().String("root", ".", "root directory to scan for terragrunt units")
	driftCmd.Flags().StringP("output", "o", "", "write drift report to file")
	rootCmd.AddCommand(driftCmd)
}

func runDrift(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	root, _ := cmd.Flags().GetString("root")
	units, err := discoverUnits(root)
	if err != nil {
		return fmt.Errorf("discovering units: %w", err)
	}

	if len(units) == 0 {
		fmt.Fprintln(os.Stderr, "No Terragrunt units found.")
		return nil
	}

	fmt.Fprintf(os.Stderr, "Found %d Terragrunt units, checking for drift...\n", len(units))

	r := runner.New(cfg.TFBinary, cfg.TGBinary)
	var drifted []plan.UnitResult

	for _, unitPath := range units {
		fmt.Fprintf(os.Stderr, "  Checking %s...\n", unitPath)
		result, runErr := r.Run(unitPath, "plan", 0)
		if runErr != nil {
			drifted = append(drifted, plan.UnitResult{
				Path:   unitPath,
				Status: plan.StatusError,
				Output: runErr.Error(),
			})
			continue
		}

		if !result.Success {
			drifted = append(drifted, plan.UnitResult{
				Path:   unitPath,
				Status: plan.StatusError,
				Output: result.CombinedOutput(),
			})
			continue
		}

		parsed := runner.ParsePlanOutput(result.CombinedOutput())
		if parsed.HasChanges() {
			drifted = append(drifted, plan.UnitResult{
				Path:    unitPath,
				Status:  plan.StatusDrift,
				Output:  result.CombinedOutput(),
				Summary: parsed,
			})
		}
	}

	if len(drifted) == 0 {
		fmt.Fprintln(os.Stderr, "No drift detected.")
		return nil
	}

	report := plan.FormatDriftReport(drifted)

	outPath, _ := cmd.Flags().GetString("output")
	if outPath != "" {
		return os.WriteFile(outPath, []byte(report), 0644)
	}

	fmt.Println(report)
	return nil
}

func discoverUnits(root string) ([]string, error) {
	var units []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".terraform" || base == ".terragrunt-cache" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() == "terragrunt.hcl" {
			dir := filepath.Dir(path)
			// Skip root-level terragrunt.hcl (usually a root config)
			if dir == root {
				return nil
			}
			units = append(units, dir)
		}
		return nil
	})
	return units, err
}
