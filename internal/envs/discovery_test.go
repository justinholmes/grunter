package envs

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/justinholmes/grunter/internal/config"
)

func createUnit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "terragrunt.hcl"), []byte("# unit\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverUnits_Basic(t *testing.T) {
	tmp := t.TempDir()

	envPath := filepath.Join(tmp, "envs", "dev")
	createUnit(t, filepath.Join(envPath, "us-east-1", "vpc"))
	createUnit(t, filepath.Join(envPath, "us-east-1", "rds"))
	createUnit(t, filepath.Join(envPath, "eu-west-1", "vpc"))

	env := config.Environment{
		Name:    "dev",
		Path:    envPath,
		Regions: []string{"us-east-1", "eu-west-1"},
	}

	units, err := DiscoverUnits(env)
	if err != nil {
		t.Fatal(err)
	}

	if len(units) != 3 {
		t.Fatalf("expected 3 units, got %d", len(units))
	}

	names := make([]string, len(units))
	for i, u := range units {
		names[i] = u.LogicalName
	}
	sort.Strings(names)

	expected := []string{
		filepath.Join("eu-west-1", "vpc"),
		filepath.Join("us-east-1", "rds"),
		filepath.Join("us-east-1", "vpc"),
	}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("expected %s, got %s", expected[i], name)
		}
	}
}

func TestDiscoverUnits_RegionExtraction(t *testing.T) {
	tmp := t.TempDir()

	envPath := filepath.Join(tmp, "envs", "dev")
	createUnit(t, filepath.Join(envPath, "us-east-1", "vpc"))
	createUnit(t, filepath.Join(envPath, "global", "iam"))

	env := config.Environment{
		Name:    "dev",
		Path:    envPath,
		Regions: []string{"us-east-1"},
	}

	units, err := DiscoverUnits(env)
	if err != nil {
		t.Fatal(err)
	}

	regionMap := make(map[string]string)
	for _, u := range units {
		regionMap[u.LogicalName] = u.Region
	}

	if r := regionMap[filepath.Join("us-east-1", "vpc")]; r != "us-east-1" {
		t.Errorf("expected region us-east-1, got %q", r)
	}
	if r := regionMap[filepath.Join("global", "iam")]; r != "" {
		t.Errorf("expected empty region for global/iam, got %q", r)
	}
}

func TestDiscoverUnits_SkipsDotDirs(t *testing.T) {
	tmp := t.TempDir()

	envPath := filepath.Join(tmp, "envs", "dev")
	createUnit(t, filepath.Join(envPath, "us-east-1", "vpc"))
	createUnit(t, filepath.Join(envPath, ".terraform", "modules"))
	createUnit(t, filepath.Join(envPath, "us-east-1", ".terragrunt-cache", "abc"))

	env := config.Environment{
		Name:    "dev",
		Path:    envPath,
		Regions: []string{"us-east-1"},
	}

	units, err := DiscoverUnits(env)
	if err != nil {
		t.Fatal(err)
	}

	if len(units) != 1 {
		t.Fatalf("expected 1 unit (skipping dot dirs), got %d", len(units))
	}
}

func TestDiscoverUnits_SkipsRootTerragruntHcl(t *testing.T) {
	tmp := t.TempDir()

	envPath := filepath.Join(tmp, "envs", "dev")
	if err := os.MkdirAll(envPath, 0755); err != nil {
		t.Fatal(err)
	}
	// Root-level terragrunt.hcl should be skipped
	if err := os.WriteFile(filepath.Join(envPath, "terragrunt.hcl"), []byte("# root\n"), 0644); err != nil {
		t.Fatal(err)
	}
	createUnit(t, filepath.Join(envPath, "us-east-1", "vpc"))

	env := config.Environment{
		Name:    "dev",
		Path:    envPath,
		Regions: []string{"us-east-1"},
	}

	units, err := DiscoverUnits(env)
	if err != nil {
		t.Fatal(err)
	}

	if len(units) != 1 {
		t.Fatalf("expected 1 unit (skipping root hcl), got %d", len(units))
	}
}

func TestExtractRegion(t *testing.T) {
	regions := []string{"us-east-1", "eu-west-1"}

	tests := []struct {
		logicalName string
		want        string
	}{
		{"us-east-1/vpc", "us-east-1"},
		{"eu-west-1/rds", "eu-west-1"},
		{"global/iam", ""},
		{"vpc", ""},
	}

	for _, tt := range tests {
		got := extractRegion(tt.logicalName, regions)
		if got != tt.want {
			t.Errorf("extractRegion(%q) = %q, want %q", tt.logicalName, got, tt.want)
		}
	}
}
