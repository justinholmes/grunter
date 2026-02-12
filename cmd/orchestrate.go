package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/justinholmes/grunter/internal/changes"
	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/deps"
	"github.com/spf13/cobra"
)

// ExecutionPlan is the JSON output of the orchestrate command.
type ExecutionPlan struct {
	Layers []ExecutionLayer `json:"layers"`
}

// ExecutionLayer is a group of units that can run in parallel.
type ExecutionLayer struct {
	Units []PlannedUnit `json:"units"`
}

// PlannedUnit is a single unit in the execution plan.
type PlannedUnit struct {
	Path       string             `json:"path"`
	ChangeType changes.ChangeType `json:"change_type"`
}

var orchestrateCmd = &cobra.Command{
	Use:   "orchestrate",
	Short: "Detect changes and output an execution plan",
	Long:  "Analyzes git diff to detect changed Terragrunt units, resolves dependency order, and outputs a JSON execution plan.",
	RunE:  runOrchestrate,
}

func init() {
	orchestrateCmd.Flags().String("base", "", "base commit SHA (default: CI_MERGE_REQUEST_DIFF_BASE_SHA)")
	orchestrateCmd.Flags().String("head", "", "head commit SHA (default: CI_COMMIT_SHA)")
	orchestrateCmd.Flags().StringP("output", "o", "", "write execution plan to file instead of stdout")
	rootCmd.AddCommand(orchestrateCmd)
}

func runOrchestrate(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	base, _ := cmd.Flags().GetString("base")
	if base == "" {
		base = os.Getenv("CI_MERGE_REQUEST_DIFF_BASE_SHA")
	}
	head, _ := cmd.Flags().GetString("head")
	if head == "" {
		head = os.Getenv("CI_COMMIT_SHA")
	}
	if base == "" || head == "" {
		return fmt.Errorf("base and head commits are required (set --base/--head or CI_MERGE_REQUEST_DIFF_BASE_SHA/CI_COMMIT_SHA)")
	}

	detector := changes.NewDetector(cfg.Ignore)
	changed, err := detector.Detect(base, head)
	if err != nil {
		return fmt.Errorf("detecting changes: %w", err)
	}

	if len(changed) == 0 {
		fmt.Fprintln(os.Stderr, "No infrastructure changes detected.")
		plan := ExecutionPlan{Layers: []ExecutionLayer{}}
		return outputPlan(cmd, plan)
	}

	unitPaths := make([]string, 0, len(changed))
	for _, c := range changed {
		unitPaths = append(unitPaths, c.UnitPath)
	}

	graph, err := deps.BuildGraph(unitPaths)
	if err != nil {
		return fmt.Errorf("building dependency graph: %w", err)
	}

	layers := graph.ExecutionLayers()

	changeLookup := make(map[string]changes.ChangeType)
	for _, c := range changed {
		changeLookup[c.UnitPath] = c.Type
	}

	plan := ExecutionPlan{Layers: make([]ExecutionLayer, 0, len(layers))}
	for _, layer := range layers {
		el := ExecutionLayer{Units: make([]PlannedUnit, 0, len(layer))}
		for _, unitPath := range layer {
			ct := changeLookup[unitPath]
			el.Units = append(el.Units, PlannedUnit{Path: unitPath, ChangeType: ct})
		}
		plan.Layers = append(plan.Layers, el)
	}

	return outputPlan(cmd, plan)
}

func outputPlan(cmd *cobra.Command, plan ExecutionPlan) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}

	outPath, _ := cmd.Flags().GetString("output")
	if outPath != "" {
		return os.WriteFile(outPath, data, 0644)
	}

	fmt.Println(string(data))
	return nil
}
