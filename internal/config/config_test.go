package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_DefaultsWhenNoFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yml")
	if err != nil {
		t.Fatal(err)
	}

	if cfg.DeployBranch != "main" {
		t.Errorf("expected deploy_branch=main, got %s", cfg.DeployBranch)
	}
	if cfg.TFBinary != "opentofu" {
		t.Errorf("expected tf_binary=opentofu, got %s", cfg.TFBinary)
	}
	if cfg.TGBinary != "terragrunt" {
		t.Errorf("expected tg_binary=terragrunt, got %s", cfg.TGBinary)
	}
	if len(cfg.Ignore) != 2 {
		t.Errorf("expected 2 ignore patterns, got %d", len(cfg.Ignore))
	}
}

func TestLoad_ParsesYAML(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
deploy_branch: develop
tf_binary: terraform
tg_binary: /usr/local/bin/terragrunt
ignore:
  - "**/*.md"
  - "**/test/**"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.DeployBranch != "develop" {
		t.Errorf("expected deploy_branch=develop, got %s", cfg.DeployBranch)
	}
	if cfg.TFBinary != "terraform" {
		t.Errorf("expected tf_binary=terraform, got %s", cfg.TFBinary)
	}
	if cfg.TGBinary != "/usr/local/bin/terragrunt" {
		t.Errorf("expected custom tg_binary, got %s", cfg.TGBinary)
	}
	if len(cfg.Ignore) != 2 {
		t.Errorf("expected 2 ignore patterns, got %d", len(cfg.Ignore))
	}
}

func TestLoad_FillsDefaultsForEmptyFields(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	// Only set ignore, leave others empty
	content := `
ignore:
  - "**/*.bak"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.DeployBranch != "main" {
		t.Errorf("expected default deploy_branch=main, got %s", cfg.DeployBranch)
	}
	if cfg.TFBinary != "opentofu" {
		t.Errorf("expected default tf_binary=opentofu, got %s", cfg.TFBinary)
	}
}
