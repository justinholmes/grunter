package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/justinholmes/grunter/internal/changes"
	"github.com/justinholmes/grunter/internal/deps"
)

// TestIntegration_FullOrchestrationFlow creates a sample repo structure
// and verifies that change detection and dependency resolution work end-to-end.
func TestIntegration_FullOrchestrationFlow(t *testing.T) {
	tmp := t.TempDir()

	// Build a realistic repo structure:
	// envs/dev/vpc       (no deps)
	// envs/dev/rds       (depends on vpc)
	// envs/dev/app       (depends on rds)
	// envs/dev/monitoring (no deps)

	units := map[string]string{
		"envs/dev/vpc": `# VPC module`,
		"envs/dev/rds": `
dependency "vpc" {
  config_path = "../vpc"
}
`,
		"envs/dev/app": `
dependency "rds" {
  config_path = "../rds"
}
`,
		"envs/dev/monitoring": `# Monitoring - independent`,
	}

	for unitPath, hclContent := range units {
		dir := filepath.Join(tmp, unitPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "terragrunt.hcl"), []byte(hclContent), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Simulate changed files
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	detector := changes.NewDetector([]string{"**/README.md"})
	detector.GitDiffFunc = func(base, head string) ([]string, error) {
		return []string{
			"envs/dev/vpc/main.tf",
			"envs/dev/rds/terragrunt.hcl",
			"envs/dev/app/variables.tf",
			"envs/dev/monitoring/main.tf",
			"envs/dev/vpc/README.md", // should be filtered
		}, nil
	}

	detected, err := detector.Detect("base", "head")
	if err != nil {
		t.Fatal(err)
	}

	// Should detect 4 units (README.md filtered but vpc still has main.tf)
	if len(detected) != 4 {
		t.Fatalf("expected 4 detected changes, got %d", len(detected))
	}

	// Build dependency graph with full paths
	unitPaths := make([]string, 0, len(detected))
	for _, c := range detected {
		unitPaths = append(unitPaths, c.UnitPath)
	}

	graph, err := deps.BuildGraph(unitPaths)
	if err != nil {
		t.Fatal(err)
	}

	layers := graph.ExecutionLayers()

	// Verify correct layering:
	// Layer 0: vpc, monitoring (no deps)
	// Layer 1: rds (depends on vpc)
	// Layer 2: app (depends on rds)
	if len(layers) < 3 {
		t.Fatalf("expected at least 3 layers, got %d", len(layers))
	}

	// Verify topological order: vpc before rds, rds before app
	order, err := graph.TopologicalSort()
	if err != nil {
		t.Fatal(err)
	}

	indexOf := func(unitPath string) int {
		for i, p := range order {
			if p == unitPath {
				return i
			}
		}
		return -1
	}

	vpcIdx := indexOf("envs/dev/vpc")
	rdsIdx := indexOf("envs/dev/rds")
	appIdx := indexOf("envs/dev/app")

	if vpcIdx < 0 || rdsIdx < 0 || appIdx < 0 {
		t.Fatalf("missing units in sort order: vpc=%d rds=%d app=%d", vpcIdx, rdsIdx, appIdx)
	}

	if vpcIdx >= rdsIdx {
		t.Errorf("vpc (idx %d) should come before rds (idx %d)", vpcIdx, rdsIdx)
	}
	if rdsIdx >= appIdx {
		t.Errorf("rds (idx %d) should come before app (idx %d)", rdsIdx, appIdx)
	}
}
