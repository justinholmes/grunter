package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/justinholmes/grunter/internal/changes"
	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/deps"
	"github.com/justinholmes/grunter/internal/envs"
	"github.com/spf13/cobra"
)

// ProgressiveDeploymentPlan is the JSON output of the promote command.
type ProgressiveDeploymentPlan struct {
	Stages []DeploymentStage `json:"stages"`
}

// DeploymentStage is a single stage in the progressive deployment.
type DeploymentStage struct {
	Environment string           `json:"environment"`
	Region      string           `json:"region,omitempty"`
	Layers      []ExecutionLayer `json:"layers"`
	IsCanary    bool             `json:"is_canary"`
}

var promoteCmd = &cobra.Command{
	Use:   "promote",
	Short: "Generate a progressive deployment plan",
	Long:  "Compares environments in promotion order and generates a canary-style progressive deployment plan. Designed to run after merge to deploy branch.",
	RunE:  runPromote,
}

func init() {
	promoteCmd.Flags().String("from", "", "source environment (default: first in sequence)")
	promoteCmd.Flags().String("to", "", "target environment (default: last in sequence)")
	promoteCmd.Flags().Bool("dry-run", false, "print plan without executing")
	promoteCmd.Flags().Bool("json", false, "output as JSON")
	promoteCmd.Flags().Bool("pipeline", false, "output as GitLab CI child pipeline YAML")
	promoteCmd.Flags().Bool("canary", false, "only include canary stages (for split pipeline generation)")
	promoteCmd.Flags().Bool("after-canary", false, "only include post-canary stages (for split pipeline generation)")
	promoteCmd.Flags().StringP("output", "o", "", "write plan to file instead of stdout")
	rootCmd.AddCommand(promoteCmd)
}

func runPromote(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.Environments) < 2 {
		return fmt.Errorf("at least 2 environments must be defined for promotion")
	}

	fromName, _ := cmd.Flags().GetString("from")
	toName, _ := cmd.Flags().GetString("to")

	fromIdx := 0
	toIdx := len(cfg.Environments) - 1

	if fromName != "" {
		fromIdx = cfg.EnvironmentIndex(fromName)
		if fromIdx < 0 {
			return fmt.Errorf("unknown environment %q", fromName)
		}
	}
	if toName != "" {
		toIdx = cfg.EnvironmentIndex(toName)
		if toIdx < 0 {
			return fmt.Errorf("unknown environment %q", toName)
		}
	}

	if fromIdx >= toIdx {
		return fmt.Errorf("source environment must come before target in promotion sequence")
	}

	var allStages []DeploymentStage

	// For each adjacent pair in the range, build promotion plan
	for i := fromIdx; i < toIdx; i++ {
		source := cfg.Environments[i]
		target := cfg.Environments[i+1]

		plan, promoteErr := envs.BuildPromotionPlan(source, target)
		if promoteErr != nil {
			return fmt.Errorf("building promotion plan %s → %s: %w", source.Name, target.Name, promoteErr)
		}

		stages, stageErr := buildDeploymentStages(plan, target)
		if stageErr != nil {
			return fmt.Errorf("building stages for %s: %w", target.Name, stageErr)
		}

		allStages = append(allStages, stages...)
	}

	// Filter stages by canary/after-canary flags
	canaryOnly, _ := cmd.Flags().GetBool("canary")
	afterCanaryOnly, _ := cmd.Flags().GetBool("after-canary")

	if canaryOnly || afterCanaryOnly {
		var filtered []DeploymentStage
		for _, s := range allStages {
			if canaryOnly && s.IsCanary {
				filtered = append(filtered, s)
			}
			if afterCanaryOnly && !s.IsCanary {
				filtered = append(filtered, s)
			}
		}
		allStages = filtered
	}

	deployPlan := ProgressiveDeploymentPlan{Stages: allStages}

	// Pipeline mode: generate GitLab CI child pipeline YAML
	pipelineMode, _ := cmd.Flags().GetBool("pipeline")
	if pipelineMode {
		// Skip canary gate in child pipeline when stages are split
		includeCanaryGate := !canaryOnly && !afterCanaryOnly
		yaml := generatePipelineYAML(&deployPlan, cfg, cfgPath, includeCanaryGate)
		outPath, _ := cmd.Flags().GetString("output")
		if outPath != "" {
			return os.WriteFile(outPath, []byte(yaml), 0644)
		}
		fmt.Print(yaml)
		return nil
	}

	data, err := json.MarshalIndent(deployPlan, "", "  ")
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

// buildDeploymentStages converts a promotion plan into deployment stages for
// the target environment. Multi-region environments are split into per-region
// stages where the first region is the canary.
func buildDeploymentStages(plan *envs.PromotionPlan, target config.Environment) ([]DeploymentStage, error) {
	// Collect all unit paths in the target that have changes
	var unitPaths []string
	changeTypes := make(map[string]changes.ChangeType)

	for _, cu := range plan.ChangedUnits {
		// Map logical name to target path
		targetPath := filepath.Join(target.Path, cu.LogicalName)
		unitPaths = append(unitPaths, targetPath)
		changeTypes[targetPath] = changes.ModuleChanged
	}
	for _, nu := range plan.NewUnits {
		targetPath := filepath.Join(target.Path, nu.LogicalName)
		// Only include new units if the target directory exists on disk
		// (i.e., the user has already created the scaffolding in their PR)
		if _, err := os.Stat(targetPath); err == nil {
			unitPaths = append(unitPaths, targetPath)
			changeTypes[targetPath] = changes.ModuleAdded
		}
	}

	// If there are actual changes, also include orphaned units (units in the
	// target that don't exist in the source). This ensures all regions of a
	// multi-region target are deployed — the canary region deploys first, and
	// after approval, remaining regions (which may only exist in the target)
	// deploy too. Without this, the canary gate has nothing to block.
	if len(unitPaths) > 0 {
		for _, ou := range plan.OrphanedUnits {
			if _, exists := changeTypes[ou.FullPath]; !exists {
				unitPaths = append(unitPaths, ou.FullPath)
				changeTypes[ou.FullPath] = changes.ModuleChanged
			}
		}
	}

	if len(unitPaths) == 0 {
		return nil, nil
	}

	// Build dep graph and get execution layers
	// Note: BuildGraph may fail if target paths don't exist on disk yet.
	// In that case, fall back to a single layer with all units.
	layers, err := buildLayersForUnits(unitPaths, changeTypes)
	if err != nil {
		// Fallback: single layer
		layer := ExecutionLayer{Units: make([]PlannedUnit, 0, len(unitPaths))}
		for _, p := range unitPaths {
			layer.Units = append(layer.Units, PlannedUnit{Path: p, ChangeType: changeTypes[p]})
		}
		layers = []ExecutionLayer{layer}
	}

	// If no regions or single region, emit one stage
	if len(target.Regions) <= 1 {
		region := ""
		if len(target.Regions) == 1 {
			region = target.Regions[0]
		}
		return []DeploymentStage{
			{
				Environment: target.Name,
				Region:      region,
				Layers:      layers,
				IsCanary:    false,
			},
		}, nil
	}

	// Multi-region: split into per-region stages
	var stages []DeploymentStage
	for i, region := range target.Regions {
		regionLayers := filterLayersByRegion(layers, target.Path, region)
		if len(regionLayers) == 0 {
			continue
		}
		stages = append(stages, DeploymentStage{
			Environment: target.Name,
			Region:      region,
			Layers:      regionLayers,
			IsCanary:    i == 0, // first region is canary
		})
	}

	// Units without a region match go into a non-region stage at the end
	nonRegionLayers := filterLayersExcludingRegions(layers, target.Path, target.Regions)
	if len(nonRegionLayers) > 0 {
		stages = append(stages, DeploymentStage{
			Environment: target.Name,
			Layers:      nonRegionLayers,
			IsCanary:    false,
		})
	}

	return stages, nil
}

func buildLayersForUnits(unitPaths []string, changeTypes map[string]changes.ChangeType) ([]ExecutionLayer, error) {
	graph, err := deps.BuildGraph(unitPaths)
	if err != nil {
		return nil, err
	}

	rawLayers := graph.ExecutionLayers()
	layers := make([]ExecutionLayer, 0, len(rawLayers))
	for _, rawLayer := range rawLayers {
		el := ExecutionLayer{Units: make([]PlannedUnit, 0, len(rawLayer))}
		for _, p := range rawLayer {
			el.Units = append(el.Units, PlannedUnit{Path: p, ChangeType: changeTypes[p]})
		}
		layers = append(layers, el)
	}
	return layers, nil
}

// filterLayersByRegion returns only units whose path contains the region
// segment under the environment path.
func filterLayersByRegion(layers []ExecutionLayer, envPath, region string) []ExecutionLayer {
	prefix := filepath.Join(envPath, region) + string(filepath.Separator)

	var result []ExecutionLayer
	for _, layer := range layers {
		var units []PlannedUnit
		for _, u := range layer.Units {
			if strings.HasPrefix(u.Path, prefix) {
				units = append(units, u)
			}
		}
		if len(units) > 0 {
			result = append(result, ExecutionLayer{Units: units})
		}
	}
	return result
}

// filterLayersExcludingRegions returns units that don't match any region prefix.
func filterLayersExcludingRegions(layers []ExecutionLayer, envPath string, regions []string) []ExecutionLayer {
	prefixes := make([]string, len(regions))
	for i, r := range regions {
		prefixes[i] = filepath.Join(envPath, r) + string(filepath.Separator)
	}

	var result []ExecutionLayer
	for _, layer := range layers {
		var units []PlannedUnit
		for _, u := range layer.Units {
			matched := false
			for _, prefix := range prefixes {
				if strings.HasPrefix(u.Path, prefix) {
					matched = true
					break
				}
			}
			if !matched {
				units = append(units, u)
			}
		}
		if len(units) > 0 {
			result = append(result, ExecutionLayer{Units: units})
		}
	}
	return result
}
