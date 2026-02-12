package deps

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestBuildGraph_NoDependencies(t *testing.T) {
	tmp := t.TempDir()

	// Create two independent units
	for _, name := range []string{"vpc", "rds"} {
		dir := filepath.Join(tmp, name)
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "terragrunt.hcl"), []byte(`
# no dependencies
`), 0644)
	}

	g, err := BuildGraph([]string{
		filepath.Join(tmp, "vpc"),
		filepath.Join(tmp, "rds"),
	})
	if err != nil {
		t.Fatal(err)
	}

	layers := g.ExecutionLayers()
	if len(layers) != 1 {
		t.Fatalf("expected 1 layer for independent units, got %d", len(layers))
	}
	if len(layers[0]) != 2 {
		t.Fatalf("expected 2 units in layer, got %d", len(layers[0]))
	}
}

func TestBuildGraph_LinearDependency(t *testing.T) {
	tmp := t.TempDir()

	// vpc (no deps) -> rds (depends on vpc) -> app (depends on rds)
	vpcDir := filepath.Join(tmp, "vpc")
	rdsDir := filepath.Join(tmp, "rds")
	appDir := filepath.Join(tmp, "app")

	os.MkdirAll(vpcDir, 0755)
	os.MkdirAll(rdsDir, 0755)
	os.MkdirAll(appDir, 0755)

	os.WriteFile(filepath.Join(vpcDir, "terragrunt.hcl"), []byte(`# no deps`), 0644)

	os.WriteFile(filepath.Join(rdsDir, "terragrunt.hcl"), []byte(`
dependency "vpc" {
  config_path = "../vpc"
}
`), 0644)

	os.WriteFile(filepath.Join(appDir, "terragrunt.hcl"), []byte(`
dependency "rds" {
  config_path = "../rds"
}
`), 0644)

	g, err := BuildGraph([]string{vpcDir, rdsDir, appDir})
	if err != nil {
		t.Fatal(err)
	}

	layers := g.ExecutionLayers()
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers for linear chain, got %d", len(layers))
	}

	// First layer should be vpc, second rds, third app
	if layers[0][0] != vpcDir {
		t.Errorf("expected vpc in layer 0, got %s", layers[0][0])
	}
	if layers[1][0] != rdsDir {
		t.Errorf("expected rds in layer 1, got %s", layers[1][0])
	}
	if layers[2][0] != appDir {
		t.Errorf("expected app in layer 2, got %s", layers[2][0])
	}
}

func TestBuildGraph_DiamondDependency(t *testing.T) {
	tmp := t.TempDir()

	// vpc -> rds, vpc -> cache, rds+cache -> app
	vpcDir := filepath.Join(tmp, "vpc")
	rdsDir := filepath.Join(tmp, "rds")
	cacheDir := filepath.Join(tmp, "cache")
	appDir := filepath.Join(tmp, "app")

	for _, d := range []string{vpcDir, rdsDir, cacheDir, appDir} {
		os.MkdirAll(d, 0755)
	}

	os.WriteFile(filepath.Join(vpcDir, "terragrunt.hcl"), []byte(`# no deps`), 0644)

	os.WriteFile(filepath.Join(rdsDir, "terragrunt.hcl"), []byte(`
dependency "vpc" {
  config_path = "../vpc"
}
`), 0644)

	os.WriteFile(filepath.Join(cacheDir, "terragrunt.hcl"), []byte(`
dependency "vpc" {
  config_path = "../vpc"
}
`), 0644)

	os.WriteFile(filepath.Join(appDir, "terragrunt.hcl"), []byte(`
dependency "rds" {
  config_path = "../rds"
}

dependency "cache" {
  config_path = "../cache"
}
`), 0644)

	g, err := BuildGraph([]string{vpcDir, rdsDir, cacheDir, appDir})
	if err != nil {
		t.Fatal(err)
	}

	layers := g.ExecutionLayers()
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers for diamond, got %d", len(layers))
	}

	// Layer 0: vpc
	if len(layers[0]) != 1 || layers[0][0] != vpcDir {
		t.Errorf("expected [vpc] in layer 0, got %v", layers[0])
	}

	// Layer 1: rds and cache (parallel)
	layer1 := make([]string, len(layers[1]))
	copy(layer1, layers[1])
	sort.Strings(layer1)
	expected1 := []string{cacheDir, rdsDir}
	sort.Strings(expected1)
	if len(layer1) != 2 || layer1[0] != expected1[0] || layer1[1] != expected1[1] {
		t.Errorf("expected [cache, rds] in layer 1, got %v", layers[1])
	}

	// Layer 2: app
	if len(layers[2]) != 1 || layers[2][0] != appDir {
		t.Errorf("expected [app] in layer 2, got %v", layers[2])
	}
}

func TestBuildGraph_DependenciesBlock(t *testing.T) {
	tmp := t.TempDir()

	vpcDir := filepath.Join(tmp, "vpc")
	rdsDir := filepath.Join(tmp, "rds")
	appDir := filepath.Join(tmp, "app")

	for _, d := range []string{vpcDir, rdsDir, appDir} {
		os.MkdirAll(d, 0755)
	}

	os.WriteFile(filepath.Join(vpcDir, "terragrunt.hcl"), []byte(`# no deps`), 0644)
	os.WriteFile(filepath.Join(rdsDir, "terragrunt.hcl"), []byte(`# no deps`), 0644)

	// Use the `dependencies` block with paths list
	os.WriteFile(filepath.Join(appDir, "terragrunt.hcl"), []byte(`
dependencies {
  paths = ["../vpc", "../rds"]
}
`), 0644)

	g, err := BuildGraph([]string{vpcDir, rdsDir, appDir})
	if err != nil {
		t.Fatal(err)
	}

	layers := g.ExecutionLayers()
	if len(layers) != 2 {
		t.Fatalf("expected 2 layers, got %d", len(layers))
	}

	// Layer 0: vpc and rds
	if len(layers[0]) != 2 {
		t.Errorf("expected 2 units in layer 0, got %d", len(layers[0]))
	}

	// Layer 1: app
	if len(layers[1]) != 1 || layers[1][0] != appDir {
		t.Errorf("expected [app] in layer 1, got %v", layers[1])
	}
}

func TestTopologicalSort_DetectsCycle(t *testing.T) {
	// Manually construct a graph with a cycle
	g := &Graph{
		nodes: map[string]bool{"a": true, "b": true},
		edges: map[string]map[string]bool{
			"a": {"b": true},
			"b": {"a": true},
		},
	}

	_, err := g.TopologicalSort()
	if err == nil {
		t.Fatal("expected error for cyclic graph")
	}
}

func TestResolveDependencyPath(t *testing.T) {
	tests := []struct {
		unitPath string
		depPath  string
		expected string
	}{
		{"envs/dev/vpc", "../rds", "envs/dev/rds"},
		{"envs/dev/vpc", "../../prod/vpc", "envs/prod/vpc"},
		{"/abs/path/vpc", "../rds", "/abs/path/rds"},
	}

	for _, tt := range tests {
		got := resolveDependencyPath(tt.unitPath, tt.depPath)
		if got != tt.expected {
			t.Errorf("resolveDependencyPath(%q, %q) = %q, want %q",
				tt.unitPath, tt.depPath, got, tt.expected)
		}
	}
}
