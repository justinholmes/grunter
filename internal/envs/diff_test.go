package envs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/justinholmes/grunter/internal/config"
)

func setupDiffEnvs(t *testing.T) (config.Environment, config.Environment) {
	t.Helper()
	tmp := t.TempDir()

	devPath := filepath.Join(tmp, "envs", "dev")
	prodPath := filepath.Join(tmp, "envs", "prod")

	// Shared unit with same content
	writeUnit(t, filepath.Join(devPath, "us-east-1", "vpc"), "# vpc config\n")
	writeUnit(t, filepath.Join(prodPath, "us-east-1", "vpc"), "# vpc config\n")

	// Shared unit with different content
	writeUnit(t, filepath.Join(devPath, "us-east-1", "rds"), "# rds dev\ninstance_type = \"t3.micro\"\n")
	writeUnit(t, filepath.Join(prodPath, "us-east-1", "rds"), "# rds prod\ninstance_type = \"r5.large\"\n")

	// Only in dev
	writeUnit(t, filepath.Join(devPath, "us-east-1", "debug"), "# debug tooling\n")

	// Only in prod
	writeUnit(t, filepath.Join(prodPath, "us-east-1", "monitoring"), "# monitoring\n")

	dev := config.Environment{Name: "dev", Path: devPath, Regions: []string{"us-east-1"}}
	prod := config.Environment{Name: "prod", Path: prodPath, Regions: []string{"us-east-1"}}

	return dev, prod
}

func writeUnit(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "terragrunt.hcl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDiff_StructuralDifferences(t *testing.T) {
	dev, prod := setupDiffEnvs(t)

	result, err := Diff(dev, prod)
	if err != nil {
		t.Fatal(err)
	}

	if result.SourceEnv != "dev" {
		t.Errorf("expected source env=dev, got %s", result.SourceEnv)
	}

	if len(result.Structural.OnlyInSource) != 1 {
		t.Fatalf("expected 1 unit only in source, got %d", len(result.Structural.OnlyInSource))
	}
	if result.Structural.OnlyInSource[0].LogicalName != filepath.Join("us-east-1", "debug") {
		t.Errorf("expected debug unit only in source, got %s", result.Structural.OnlyInSource[0].LogicalName)
	}

	if len(result.Structural.OnlyInTarget) != 1 {
		t.Fatalf("expected 1 unit only in target, got %d", len(result.Structural.OnlyInTarget))
	}
	if result.Structural.OnlyInTarget[0].LogicalName != filepath.Join("us-east-1", "monitoring") {
		t.Errorf("expected monitoring unit only in target, got %s", result.Structural.OnlyInTarget[0].LogicalName)
	}
}

func TestDiff_ContentDifferences(t *testing.T) {
	dev, prod := setupDiffEnvs(t)

	result, err := Diff(dev, prod)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.ContentDiffs) != 2 {
		t.Fatalf("expected 2 content diffs, got %d", len(result.ContentDiffs))
	}

	diffMap := make(map[string]ContentDiff)
	for _, cd := range result.ContentDiffs {
		diffMap[cd.LogicalName] = cd
	}

	vpcDiff := diffMap[filepath.Join("us-east-1", "vpc")]
	if !vpcDiff.Identical {
		t.Error("expected vpc diff to be identical")
	}

	rdsDiff := diffMap[filepath.Join("us-east-1", "rds")]
	if rdsDiff.Identical {
		t.Error("expected rds diff to NOT be identical")
	}
	if rdsDiff.Unified == "" {
		t.Error("expected non-empty unified diff for rds")
	}
}

func TestDiff_EmptyEnvironments(t *testing.T) {
	tmp := t.TempDir()

	devPath := filepath.Join(tmp, "envs", "dev")
	prodPath := filepath.Join(tmp, "envs", "prod")
	os.MkdirAll(devPath, 0755)
	os.MkdirAll(prodPath, 0755)

	dev := config.Environment{Name: "dev", Path: devPath}
	prod := config.Environment{Name: "prod", Path: prodPath}

	result, err := Diff(dev, prod)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Structural.OnlyInSource) != 0 {
		t.Errorf("expected 0 only in source, got %d", len(result.Structural.OnlyInSource))
	}
	if len(result.ContentDiffs) != 0 {
		t.Errorf("expected 0 content diffs, got %d", len(result.ContentDiffs))
	}
}
