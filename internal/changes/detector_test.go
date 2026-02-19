package changes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetect_ClassifiesChanges(t *testing.T) {
	// Create a temp dir structure simulating a terragrunt repo
	tmp := t.TempDir()

	// Create unit directories with terragrunt.hcl files
	units := []string{
		"envs/dev/vpc",
		"envs/dev/rds",
		"envs/prod/vpc",
	}
	for _, u := range units {
		dir := filepath.Join(tmp, u)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "terragrunt.hcl"), []byte("# unit"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Change working directory to temp dir for resolveUnit to work
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	d := NewDetector(nil)
	// Override git diff to return mock file list
	d.GitDiffFunc = func(base, head string) ([]string, error) {
		return []string{
			"envs/dev/vpc/terragrunt.hcl",
			"envs/dev/rds/main.tf",
			"envs/prod/vpc/variables.tf",
		}, nil
	}

	changes, err := d.Detect("abc123", "def456")
	if err != nil {
		t.Fatal(err)
	}

	if len(changes) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(changes))
	}

	// Verify each unit was detected
	found := make(map[string]bool)
	for _, c := range changes {
		found[c.UnitPath] = true
	}

	for _, u := range units {
		if !found[u] {
			t.Errorf("expected unit %s to be detected", u)
		}
	}
}

func TestDetect_RespectsIgnorePatterns(t *testing.T) {
	tmp := t.TempDir()

	dir := filepath.Join(tmp, "envs/dev/vpc")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "terragrunt.hcl"), []byte("# unit"), 0644)

	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	d := NewDetector([]string{"**/README.md", "**/.terraform.lock.hcl"})
	d.GitDiffFunc = func(base, head string) ([]string, error) {
		return []string{
			"envs/dev/vpc/README.md",
			"envs/dev/vpc/.terraform.lock.hcl",
		}, nil
	}

	changes, err := d.Detect("abc123", "def456")
	if err != nil {
		t.Fatal(err)
	}

	if len(changes) != 0 {
		t.Fatalf("expected 0 changes after filtering, got %d", len(changes))
	}
}

func TestDetect_DeduplicatesMultipleFilesInSameUnit(t *testing.T) {
	tmp := t.TempDir()

	dir := filepath.Join(tmp, "envs/dev/vpc")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "terragrunt.hcl"), []byte("# unit"), 0644)

	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	d := NewDetector(nil)
	d.GitDiffFunc = func(base, head string) ([]string, error) {
		return []string{
			"envs/dev/vpc/terragrunt.hcl",
			"envs/dev/vpc/variables.tf",
			"envs/dev/vpc/main.tf",
		}, nil
	}

	changes, err := d.Detect("abc123", "def456")
	if err != nil {
		t.Fatal(err)
	}

	if len(changes) != 1 {
		t.Fatalf("expected 1 deduplicated change, got %d", len(changes))
	}
	if len(changes[0].Files) != 3 {
		t.Fatalf("expected 3 files in change, got %d", len(changes[0].Files))
	}
}

func TestDetect_EnvCommonChanges(t *testing.T) {
	tmp := t.TempDir()

	// Create _envcommon and a unit
	os.MkdirAll(filepath.Join(tmp, "_envcommon"), 0755)
	os.WriteFile(filepath.Join(tmp, "_envcommon/vpc.hcl"), []byte("# common"), 0644)

	unitDir := filepath.Join(tmp, "envs/dev/vpc")
	os.MkdirAll(unitDir, 0755)
	os.WriteFile(filepath.Join(unitDir, "terragrunt.hcl"), []byte("# unit"), 0644)

	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	d := NewDetector(nil)
	d.GitDiffFunc = func(base, head string) ([]string, error) {
		return []string{
			"_envcommon/vpc.hcl",
		}, nil
	}

	changes, err := d.Detect("abc123", "def456")
	if err != nil {
		t.Fatal(err)
	}

	if len(changes) == 0 {
		t.Fatal("expected at least 1 change for _envcommon modification")
	}
	for _, c := range changes {
		if c.Type != EnvCommonChanged {
			t.Errorf("expected EnvCommonChanged, got %s", c.Type)
		}
	}
}

func TestDetect_SharedModuleChanges(t *testing.T) {
	tmp := t.TempDir()

	// Create a shared module with .tf files
	moduleDir := filepath.Join(tmp, "modules/routing/queues")
	os.MkdirAll(moduleDir, 0755)
	os.WriteFile(filepath.Join(moduleDir, "main.tf"), []byte("resource \"null_resource\" \"q\" {}"), 0644)

	// Create units that source from the shared module
	units := []string{
		"envs/staging/eu-west-1/routing/queues",
		"envs/prod/eu-west-1/routing/queues",
	}
	for _, u := range units {
		dir := filepath.Join(tmp, u)
		os.MkdirAll(dir, 0755)
		// source is a relative path from the unit to the module
		rel, _ := filepath.Rel(dir, moduleDir)
		hcl := "include \"root\" {\n  path = find_in_parent_folders(\"root.hcl\")\n}\n\nterraform {\n  source = \"" + rel + "\"\n}\n"
		os.WriteFile(filepath.Join(dir, "terragrunt.hcl"), []byte(hcl), 0644)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	d := NewDetector(nil)
	d.GitDiffFunc = func(base, head string) ([]string, error) {
		return []string{
			"modules/routing/queues/main.tf",
		}, nil
	}

	changes, err := d.Detect("abc123", "def456")
	if err != nil {
		t.Fatal(err)
	}

	if len(changes) != 2 {
		t.Fatalf("expected 2 changes from shared module, got %d", len(changes))
	}

	found := make(map[string]bool)
	for _, c := range changes {
		found[c.UnitPath] = true
		if c.Type != SourceChanged {
			t.Errorf("expected SourceChanged for %s, got %s", c.UnitPath, c.Type)
		}
	}

	for _, u := range units {
		if !found[u] {
			t.Errorf("expected unit %s to be detected", u)
		}
	}
}

func TestFilterIgnored(t *testing.T) {
	d := &Detector{
		ignorePatterns: []string{"**/README.md", "**/.terraform.lock.hcl"},
	}

	files := []string{
		"envs/dev/vpc/main.tf",
		"envs/dev/vpc/README.md",
		"envs/dev/vpc/.terraform.lock.hcl",
		"envs/dev/rds/terragrunt.hcl",
	}

	filtered := d.filterIgnored(files)

	if len(filtered) != 2 {
		t.Fatalf("expected 2 files after filtering, got %d: %v", len(filtered), filtered)
	}
}
