//go:build integration

package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justinholmes/grunter/internal/changes"
	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/deps"
	"github.com/justinholmes/grunter/internal/plan"
	"github.com/justinholmes/grunter/internal/runner"
)

// localTF creates a terraform config using only null_resource (no cloud creds needed).
func localTF(trigger string) string {
	return `
terraform {
  required_providers {
    null = {
      source  = "hashicorp/null"
      version = "~> 3.0"
    }
  }
  backend "local" {}
}

resource "null_resource" "main" {
  triggers = {
    value = "` + trigger + `"
  }
}

output "unit_name" {
  value = "` + trigger + `"
}
`
}

// TestE2E_FullOrchestratePlanApplyFlow tests the complete lifecycle:
// 1. Create a git repo with multiple terragrunt units (with dependencies)
// 2. Apply initial state
// 3. Make changes on a branch
// 4. Detect changes via git diff
// 5. Build dependency graph and execution plan
// 6. Run plan on changed units
// 7. Run apply on changed units
// 8. Verify state reflects changes
func TestE2E_FullOrchestratePlanApplyFlow(t *testing.T) {
	RequireBinary(t, "terragrunt")
	RequireBinary(t, "tofu")
	RequireBinary(t, "git")

	tmp := t.TempDir()

	// Build a realistic multi-unit repo structure
	//
	// networking/vpc  (no deps)
	// data/rds        (depends on vpc)
	// app/service     (depends on rds)
	// monitoring/alerts (no deps, independent)

	vpcHCL := `# No dependencies`
	rdsHCL := `
dependency "vpc" {
  config_path = "../../networking/vpc"
}
`
	serviceHCL := `
dependency "rds" {
  config_path = "../../data/rds"
}
`
	alertsHCL := `# Independent unit`

	vpcDir := CreateTerragruntUnit(t, tmp, "networking/vpc", vpcHCL, localTF("vpc-v1"))
	rdsDir := CreateTerragruntUnit(t, tmp, "data/rds", rdsHCL, localTF("rds-v1"))
	serviceDir := CreateTerragruntUnit(t, tmp, "app/service", serviceHCL, localTF("service-v1"))
	CreateTerragruntUnit(t, tmp, "monitoring/alerts", alertsHCL, localTF("alerts-v1"))

	// Write config
	os.MkdirAll(filepath.Join(tmp, ".grunter"), 0755)
	os.WriteFile(filepath.Join(tmp, ".grunter", "config.yml"), []byte(`
deploy_branch: main
tf_binary: tofu
tg_binary: terragrunt
ignore:
  - "**/README.md"
`), 0644)

	// Gitignore terraform generated files (as any real repo would)
	os.WriteFile(filepath.Join(tmp, ".gitignore"), []byte(`
.terraform/
.terragrunt-cache/
*.tfstate
*.tfstate.backup
.terraform.lock.hcl
`), 0644)

	// --- Step 1: Init git repo and apply initial state ---
	t.Log("=== Step 1: Initialize git repo and apply initial state ===")
	InitGitRepo(t, tmp)

	cfg, err := config.Load(filepath.Join(tmp, ".grunter", "config.yml"))
	if err != nil {
		t.Fatal(err)
	}

	r := runner.New(cfg.TFBinary, cfg.TGBinary)

	// Apply all units in dependency order
	for _, unitDir := range []string{vpcDir, rdsDir, serviceDir} {
		t.Logf("  Applying %s...", filepath.Base(filepath.Dir(unitDir))+"/"+filepath.Base(unitDir))
		result, err := r.Run(unitDir, "apply", 5*time.Minute)
		if err != nil {
			t.Fatalf("apply error for %s: %v", unitDir, err)
		}
		if !result.Success {
			t.Fatalf("apply failed for %s:\n%s", unitDir, result.CombinedOutput())
		}
	}

	baseSHA := GitGetHEAD(t, tmp)
	t.Logf("Base SHA: %s", baseSHA)

	// --- Step 2: Make changes on a branch ---
	t.Log("=== Step 2: Make changes ===")

	// Modify vpc and rds configs
	os.WriteFile(filepath.Join(vpcDir, "main.tf"), []byte(localTF("vpc-v2")), 0644)
	os.WriteFile(filepath.Join(rdsDir, "main.tf"), []byte(localTF("rds-v2")), 0644)

	headSHA := GitCommitAll(t, tmp, "update vpc and rds")
	t.Logf("Head SHA: %s", headSHA)

	// --- Step 3: Detect changes ---
	t.Log("=== Step 3: Detect changes ===")

	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	detector := changes.NewDetector(cfg.Ignore)
	detected, err := detector.Detect(baseSHA, headSHA)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}

	t.Logf("Detected %d changed units:", len(detected))
	for _, c := range detected {
		t.Logf("  %s (%s) - files: %v", c.UnitPath, c.Type, c.Files)
	}

	if len(detected) != 2 {
		t.Fatalf("expected 2 changed units (vpc, rds), got %d", len(detected))
	}

	// Verify correct units detected
	detectedPaths := make(map[string]bool)
	for _, c := range detected {
		detectedPaths[c.UnitPath] = true
	}
	if !detectedPaths["networking/vpc"] {
		t.Error("expected networking/vpc to be detected")
	}
	if !detectedPaths["data/rds"] {
		t.Error("expected data/rds to be detected")
	}

	// --- Step 4: Build dependency graph ---
	t.Log("=== Step 4: Build dependency graph ===")

	unitPaths := make([]string, 0, len(detected))
	for _, c := range detected {
		unitPaths = append(unitPaths, c.UnitPath)
	}

	graph, err := deps.BuildGraph(unitPaths)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}

	layers := graph.ExecutionLayers()
	t.Logf("Execution layers: %d", len(layers))
	for i, layer := range layers {
		t.Logf("  Layer %d: %v", i, layer)
	}

	sorted, err := graph.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}

	// Verify vpc comes before rds
	vpcIdx, rdsIdx := -1, -1
	for i, p := range sorted {
		if p == "networking/vpc" {
			vpcIdx = i
		}
		if p == "data/rds" {
			rdsIdx = i
		}
	}
	if vpcIdx >= rdsIdx {
		t.Errorf("vpc (idx %d) should come before rds (idx %d) in execution order", vpcIdx, rdsIdx)
	}

	// --- Step 5: Run plan on each unit in order ---
	t.Log("=== Step 5: Run plans ===")

	var planResults []plan.UnitResult
	for _, unitPath := range sorted {
		t.Logf("  Planning %s...", unitPath)
		result, err := r.Run(unitPath, "plan", 5*time.Minute)
		if err != nil {
			t.Fatalf("plan error for %s: %v", unitPath, err)
		}

		combined := result.CombinedOutput()
		summary := runner.ParsePlanOutput(combined)
		t.Logf("    Result: %s (success=%v)", summary.String(), result.Success)

		status := plan.StatusChanged
		if !result.Success {
			status = plan.StatusError
		} else if !summary.HasChanges() {
			status = plan.StatusNoChanges
		}

		planResults = append(planResults, plan.UnitResult{
			Path:    unitPath,
			Status:  status,
			Output:  combined,
			Summary: summary,
		})

		if !result.Success {
			t.Fatalf("plan failed for %s:\n%s", unitPath, combined)
		}
		if !summary.HasChanges() {
			t.Errorf("expected changes for %s but got none", unitPath)
		}
	}

	// --- Step 6: Verify plan output formatting ---
	t.Log("=== Step 6: Format plan output ===")

	formatted := plan.FormatMultiUnitComment(planResults)
	t.Logf("Formatted comment (%d bytes):\n%s", len(formatted), formatted[:min(len(formatted), 500)])

	if !strings.Contains(formatted, "networking/vpc") {
		t.Error("formatted output missing vpc")
	}
	if !strings.Contains(formatted, "data/rds") {
		t.Error("formatted output missing rds")
	}
	if !strings.Contains(formatted, "Changed") {
		t.Error("formatted output missing status")
	}

	// --- Step 7: Apply changes in dependency order ---
	t.Log("=== Step 7: Apply changes ===")

	for _, unitPath := range sorted {
		t.Logf("  Applying %s...", unitPath)
		result, err := r.Run(unitPath, "apply", 5*time.Minute)
		if err != nil {
			t.Fatalf("apply error for %s: %v", unitPath, err)
		}
		if !result.Success {
			t.Fatalf("apply failed for %s:\n%s", unitPath, result.CombinedOutput())
		}
	}

	// --- Step 8: Verify no drift after apply ---
	t.Log("=== Step 8: Verify no drift ===")

	for _, unitPath := range sorted {
		result, err := r.Run(unitPath, "plan", 5*time.Minute)
		if err != nil {
			t.Fatalf("post-apply plan error for %s: %v", unitPath, err)
		}
		summary := runner.ParsePlanOutput(result.CombinedOutput())
		if summary.HasChanges() {
			t.Errorf("drift detected in %s after apply: %s", unitPath, summary.String())
		}
	}

	t.Log("=== E2E test passed! ===")
}

// TestE2E_ExecutionPlanJSON verifies the JSON execution plan output structure.
func TestE2E_ExecutionPlanJSON(t *testing.T) {
	RequireBinary(t, "git")

	tmp := t.TempDir()

	// Create units with dependencies
	CreateTerragruntUnit(t, tmp, "vpc", `# no deps`, "")
	CreateTerragruntUnit(t, tmp, "rds", `
dependency "vpc" {
  config_path = "../vpc"
}
`, "")
	CreateTerragruntUnit(t, tmp, "app", `
dependency "rds" {
  config_path = "../rds"
}
`, "")

	// Init git and make changes
	InitGitRepo(t, tmp)
	baseSHA := GitGetHEAD(t, tmp)

	os.WriteFile(filepath.Join(tmp, "vpc", "change.tf"), []byte("# changed"), 0644)
	os.WriteFile(filepath.Join(tmp, "rds", "change.tf"), []byte("# changed"), 0644)
	os.WriteFile(filepath.Join(tmp, "app", "change.tf"), []byte("# changed"), 0644)
	headSHA := GitCommitAll(t, tmp, "change all")

	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	// Run change detection
	detector := changes.NewDetector(nil)
	detected, err := detector.Detect(baseSHA, headSHA)
	if err != nil {
		t.Fatal(err)
	}

	unitPaths := make([]string, 0, len(detected))
	for _, c := range detected {
		unitPaths = append(unitPaths, c.UnitPath)
	}

	graph, err := deps.BuildGraph(unitPaths)
	if err != nil {
		t.Fatal(err)
	}

	layers := graph.ExecutionLayers()

	// Build the execution plan JSON (same structure as orchestrate command)
	type PlannedUnit struct {
		Path       string             `json:"path"`
		ChangeType changes.ChangeType `json:"change_type"`
	}
	type ExecutionLayer struct {
		Units []PlannedUnit `json:"units"`
	}
	type ExecutionPlan struct {
		Layers []ExecutionLayer `json:"layers"`
	}

	changeLookup := make(map[string]changes.ChangeType)
	for _, c := range detected {
		changeLookup[c.UnitPath] = c.Type
	}

	execPlan := ExecutionPlan{Layers: make([]ExecutionLayer, 0, len(layers))}
	for _, layer := range layers {
		el := ExecutionLayer{Units: make([]PlannedUnit, 0, len(layer))}
		for _, p := range layer {
			el.Units = append(el.Units, PlannedUnit{Path: p, ChangeType: changeLookup[p]})
		}
		execPlan.Layers = append(execPlan.Layers, el)
	}

	data, err := json.MarshalIndent(execPlan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Execution plan JSON:\n%s", string(data))

	// Verify structure
	var parsed ExecutionPlan
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	if len(parsed.Layers) < 2 {
		t.Fatalf("expected at least 2 layers, got %d", len(parsed.Layers))
	}

	// Verify all 3 units are present
	allUnits := make(map[string]bool)
	for _, layer := range parsed.Layers {
		for _, u := range layer.Units {
			allUnits[u.Path] = true
		}
	}

	for _, expected := range []string{"vpc", "rds", "app"} {
		if !allUnits[expected] {
			t.Errorf("missing unit %s in execution plan", expected)
		}
	}

	// Verify vpc is in an earlier layer than rds, rds earlier than app
	unitLayer := make(map[string]int)
	for i, layer := range parsed.Layers {
		for _, u := range layer.Units {
			unitLayer[u.Path] = i
		}
	}

	if unitLayer["vpc"] >= unitLayer["rds"] {
		t.Errorf("vpc (layer %d) should be before rds (layer %d)", unitLayer["vpc"], unitLayer["rds"])
	}
	if unitLayer["rds"] >= unitLayer["app"] {
		t.Errorf("rds (layer %d) should be before app (layer %d)", unitLayer["rds"], unitLayer["app"])
	}
}

// TestE2E_DriftDetection tests the drift detection flow with real terraform state.
func TestE2E_DriftDetection(t *testing.T) {
	RequireBinary(t, "terragrunt")
	RequireBinary(t, "tofu")

	tmp := t.TempDir()

	// Create and apply two units
	unit1 := CreateTerragruntUnit(t, tmp, "unit1", `# no deps`, localTF("stable"))
	unit2 := CreateTerragruntUnit(t, tmp, "unit2", `# no deps`, localTF("will-drift"))

	r := runner.New("tofu", "terragrunt")

	for _, unitDir := range []string{unit1, unit2} {
		result, err := r.Run(unitDir, "apply", 5*time.Minute)
		if err != nil {
			t.Fatalf("apply error: %v", err)
		}
		if !result.Success {
			t.Fatalf("apply failed:\n%s", result.CombinedOutput())
		}
	}

	// Modify unit2's config (simulating drift: state doesn't match code)
	os.WriteFile(filepath.Join(unit2, "main.tf"), []byte(localTF("drifted-value")), 0644)

	// Check each unit for drift
	var driftResults []plan.UnitResult
	for _, unitDir := range []string{unit1, unit2} {
		result, err := r.Run(unitDir, "plan", 5*time.Minute)
		if err != nil {
			t.Fatalf("plan error: %v", err)
		}

		combined := result.CombinedOutput()
		summary := runner.ParsePlanOutput(combined)

		if summary.HasChanges() {
			driftResults = append(driftResults, plan.UnitResult{
				Path:    unitDir,
				Status:  plan.StatusDrift,
				Output:  combined,
				Summary: summary,
			})
		}
	}

	// unit1 should have no drift, unit2 should have drift
	if len(driftResults) != 1 {
		t.Fatalf("expected 1 unit with drift, got %d", len(driftResults))
	}
	if driftResults[0].Path != unit2 {
		t.Errorf("expected drift in unit2, got %s", driftResults[0].Path)
	}

	// Verify drift report formatting
	report := plan.FormatDriftReport(driftResults)
	t.Logf("Drift report:\n%s", report)

	if !strings.Contains(report, "Drift Detection Report") {
		t.Error("missing report header")
	}
	if !strings.Contains(report, "**1** unit(s)") {
		t.Error("missing unit count")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
