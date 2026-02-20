package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/justinholmes/grunter/internal/changes"
	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/lint"
	"github.com/spf13/cobra"
)

var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Run code quality checks on changed units",
	Long:  "Runs hclfmt, tofu fmt, validate, and tflint against the units in the execution plan.",
	RunE:  runLint,
}

func init() {
	lintCmd.Flags().String("input", "execution-plan.json", "path to execution-plan.json")
	lintCmd.Flags().StringP("output", "o", "", "write markdown report to file")
	lintCmd.Flags().Bool("skip-validate", false, "skip terragrunt validate")
	lintCmd.Flags().Bool("skip-tflint", false, "skip tflint")
	rootCmd.AddCommand(lintCmd)
}

func runLint(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	inputPath, _ := cmd.Flags().GetString("input")
	outputPath, _ := cmd.Flags().GetString("output")
	skipValidate, _ := cmd.Flags().GetBool("skip-validate")
	skipTFLint, _ := cmd.Flags().GetBool("skip-tflint")

	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("reading execution plan: %w", err)
	}

	var plan ExecutionPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return fmt.Errorf("parsing execution plan: %w", err)
	}

	// Collect unit paths from all layers
	var unitPaths []string
	for _, layer := range plan.Layers {
		for _, unit := range layer.Units {
			unitPaths = append(unitPaths, unit.Path)
		}
	}

	if len(unitPaths) == 0 {
		fmt.Println("Nothing to lint.")
		return nil
	}

	// Resolve module directories from unit paths
	moduleDirs := resolveModuleDirs(unitPaths)

	linter := lint.New(cfg.TFBinary, cfg.TGBinary)
	var results []lint.Result
	var failed int

	// hclfmt on each unit dir
	for _, unitPath := range unitPaths {
		r := linter.CheckHCLFmt(unitPath)
		results = append(results, r)
		if !r.Passed {
			failed++
		}
	}

	// tofu fmt on each unique module dir
	for _, moduleDir := range moduleDirs {
		r := linter.CheckTFFmt(moduleDir)
		results = append(results, r)
		if !r.Passed {
			failed++
		}
	}

	// validate on each unit dir
	if !skipValidate {
		for _, unitPath := range unitPaths {
			r := linter.Validate(unitPath)
			results = append(results, r)
			if !r.Passed {
				failed++
			}
		}
	}

	// tflint on each unique module dir
	if !skipTFLint {
		for _, moduleDir := range moduleDirs {
			r := linter.CheckTFLint(moduleDir)
			results = append(results, r)
			if !r.Passed && !r.Skipped {
				failed++
			}
		}
	}

	// Print summary to terminal
	printLintSummary(results)

	// Write markdown report if requested
	if outputPath != "" {
		report := formatLintReport(results, failed)
		if err := os.WriteFile(outputPath, []byte(report), 0644); err != nil {
			return fmt.Errorf("writing lint report: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Lint report written to %s\n", outputPath)
	}

	if failed > 0 {
		return fmt.Errorf("%d lint check(s) failed", failed)
	}

	fmt.Println("\nAll lint checks passed.")
	return nil
}

// resolveModuleDirs extracts unique terraform source directories from unit paths.
func resolveModuleDirs(unitPaths []string) []string {
	seen := make(map[string]bool)
	var dirs []string

	for _, unitPath := range unitPaths {
		hclPath := filepath.Join(unitPath, "terragrunt.hcl")
		source := changes.ExtractTerragruntSource(hclPath)
		if source == "" {
			continue
		}

		moduleDir := filepath.Join(unitPath, source)
		moduleDir = filepath.Clean(moduleDir)

		if !seen[moduleDir] {
			// Verify the directory exists
			if info, err := os.Stat(moduleDir); err == nil && info.IsDir() {
				seen[moduleDir] = true
				dirs = append(dirs, moduleDir)
			}
		}
	}

	return dirs
}

func printLintSummary(results []lint.Result) {
	fmt.Println("Lint Results:")
	fmt.Println(strings.Repeat("-", 60))

	for _, r := range results {
		status := "PASS"
		if r.Skipped {
			status = "SKIP"
		} else if !r.Passed {
			status = "FAIL"
		}
		fmt.Printf("  [%s] %-10s %s\n", status, r.Kind, r.Path)
		if !r.Passed && !r.Skipped && r.Output != "" {
			// Indent failure output
			for _, line := range strings.Split(strings.TrimSpace(r.Output), "\n") {
				fmt.Printf("         %s\n", line)
			}
		}
	}
}

// ansiRe strips ANSI escape codes from lint output.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func formatLintReport(results []lint.Result, failed int) string {
	var sb strings.Builder

	if failed == 0 {
		sb.WriteString("### :white_check_mark: Lint passed\n\n")
	} else {
		sb.WriteString(fmt.Sprintf("### :x: Lint failed (%d check(s))\n\n", failed))
	}

	// Summary table
	sb.WriteString("| Status | Check | Path |\n")
	sb.WriteString("|:------:|-------|------|\n")
	for _, r := range results {
		icon := ":white_check_mark:"
		if r.Skipped {
			icon = ":fast_forward:"
		} else if !r.Passed {
			icon = ":x:"
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | `%s` |\n", icon, r.Kind, r.Path))
	}
	sb.WriteString("\n")

	// Failure details
	hasFailures := false
	for _, r := range results {
		if r.Passed || r.Skipped || r.Output == "" {
			continue
		}
		if !hasFailures {
			sb.WriteString("**Failure details:**\n\n")
			hasFailures = true
		}
		cleaned := ansiRe.ReplaceAllString(strings.TrimSpace(r.Output), "")
		sb.WriteString(fmt.Sprintf("<details open>\n<summary><code>%s</code> — %s</summary>\n\n", r.Path, r.Kind))
		sb.WriteString("```\n")
		sb.WriteString(cleaned)
		sb.WriteString("\n```\n")
		sb.WriteString("</details>\n\n")
	}

	sb.WriteString("---\n*Posted by [Grunter](https://github.com/justinholmes/grunter)*\n")
	return sb.String()
}
