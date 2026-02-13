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

func TestLoad_ParsesEnvironments(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: dev
    path: envs/dev
    regions:
      - us-east-1
      - eu-west-1
  - name: staging
    path: envs/staging
    regions:
      - us-east-1
  - name: prod
    path: envs/prod
    regions:
      - us-east-1
      - eu-west-1
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Environments) != 3 {
		t.Fatalf("expected 3 environments, got %d", len(cfg.Environments))
	}
	if cfg.Environments[0].Name != "dev" {
		t.Errorf("expected first env name=dev, got %s", cfg.Environments[0].Name)
	}
	if cfg.Environments[0].Path != "envs/dev" {
		t.Errorf("expected first env path=envs/dev, got %s", cfg.Environments[0].Path)
	}
	if len(cfg.Environments[0].Regions) != 2 {
		t.Errorf("expected 2 regions for dev, got %d", len(cfg.Environments[0].Regions))
	}
	if cfg.Environments[2].Name != "prod" {
		t.Errorf("expected third env name=prod, got %s", cfg.Environments[2].Name)
	}
}

func TestLoad_NoEnvironments_BackwardCompatible(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `deploy_branch: main`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Environments) != 0 {
		t.Errorf("expected no environments, got %d", len(cfg.Environments))
	}
}

func TestValidate_DuplicateEnvName(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: dev
    path: envs/dev
  - name: dev
    path: envs/dev2
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for duplicate environment name")
	}
}

func TestValidate_MissingEnvName(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - path: envs/dev
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing environment name")
	}
}

func TestValidate_MissingEnvPath(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: dev
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing environment path")
	}
}

func TestGetEnvironment(t *testing.T) {
	cfg := &Config{
		Environments: []Environment{
			{Name: "dev", Path: "envs/dev"},
			{Name: "prod", Path: "envs/prod"},
		},
	}

	env := cfg.GetEnvironment("dev")
	if env == nil {
		t.Fatal("expected to find dev environment")
	}
	if env.Path != "envs/dev" {
		t.Errorf("expected path=envs/dev, got %s", env.Path)
	}

	if cfg.GetEnvironment("nonexistent") != nil {
		t.Error("expected nil for nonexistent environment")
	}
}

func TestEnvironmentIndex(t *testing.T) {
	cfg := &Config{
		Environments: []Environment{
			{Name: "dev", Path: "envs/dev"},
			{Name: "staging", Path: "envs/staging"},
			{Name: "prod", Path: "envs/prod"},
		},
	}

	if idx := cfg.EnvironmentIndex("dev"); idx != 0 {
		t.Errorf("expected dev at index 0, got %d", idx)
	}
	if idx := cfg.EnvironmentIndex("prod"); idx != 2 {
		t.Errorf("expected prod at index 2, got %d", idx)
	}
	if idx := cfg.EnvironmentIndex("nonexistent"); idx != -1 {
		t.Errorf("expected -1 for nonexistent, got %d", idx)
	}
}

func TestLoad_ParsesRFCConfig_GitLab(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: staging
    path: envs/staging
    rfc:
      source: gitlab
      approved_label: rfc-approved
      issue_approved_label: "status::approved"
      emergency_label: emergency
    deployment_window:
      days: [monday, tuesday, wednesday, thursday, friday]
      start: "09:00"
      end: "17:00"
      timezone: UTC
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	env := cfg.GetEnvironment("staging")
	if env == nil {
		t.Fatal("expected staging environment")
	}
	if !env.RequiresRFCCheck() {
		t.Error("expected RequiresRFCCheck to be true")
	}
	if env.RFC.Source != RFCSourceGitLab {
		t.Errorf("expected source=gitlab, got %s", env.RFC.Source)
	}
	if env.RFC.ApprovedLabel != "rfc-approved" {
		t.Errorf("expected approved_label=rfc-approved, got %s", env.RFC.ApprovedLabel)
	}
	if env.RFC.IssueApprovedLabel != "status::approved" {
		t.Errorf("expected issue_approved_label=status::approved, got %s", env.RFC.IssueApprovedLabel)
	}
	if env.RFC.EmergencyLabel != "emergency" {
		t.Errorf("expected emergency_label=emergency, got %s", env.RFC.EmergencyLabel)
	}
	if !env.HasDeploymentWindow() {
		t.Error("expected HasDeploymentWindow to be true")
	}
	if len(env.DeploymentWindow.Days) != 5 {
		t.Errorf("expected 5 days, got %d", len(env.DeploymentWindow.Days))
	}
	if env.DeploymentWindow.Start != "09:00" {
		t.Errorf("expected start=09:00, got %s", env.DeploymentWindow.Start)
	}
	if env.DeploymentWindow.Timezone != "UTC" {
		t.Errorf("expected timezone=UTC, got %s", env.DeploymentWindow.Timezone)
	}
}

func TestLoad_ParsesRFCConfig_ServiceNow(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: prod
    path: envs/prod
    rfc:
      source: servicenow
      instance_url: https://myco.service-now.com
      change_number_var: SNOW_CHG_NUMBER
      emergency_label: emergency
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	env := cfg.GetEnvironment("prod")
	if env == nil {
		t.Fatal("expected prod environment")
	}
	if env.RFC.Source != RFCSourceServiceNow {
		t.Errorf("expected source=servicenow, got %s", env.RFC.Source)
	}
	if env.RFC.InstanceURL != "https://myco.service-now.com" {
		t.Errorf("unexpected instance_url: %s", env.RFC.InstanceURL)
	}
	if env.RFC.ChangeNumberVar != "SNOW_CHG_NUMBER" {
		t.Errorf("unexpected change_number_var: %s", env.RFC.ChangeNumberVar)
	}
}

func TestLoad_NoRFCConfig_BackwardCompatible(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: dev
    path: envs/dev
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	env := cfg.GetEnvironment("dev")
	if env.RequiresRFCCheck() {
		t.Error("expected RequiresRFCCheck to be false with no RFC config")
	}
	if env.HasDeploymentWindow() {
		t.Error("expected HasDeploymentWindow to be false with no window config")
	}
}

func TestValidate_RFCGitLab_MissingApprovedLabel(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: staging
    path: envs/staging
    rfc:
      source: gitlab
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing approved_label")
	}
}

func TestValidate_RFCServiceNow_MissingInstanceURL(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: prod
    path: envs/prod
    rfc:
      source: servicenow
      change_number_var: SNOW_CHG_NUMBER
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing instance_url")
	}
}

func TestValidate_RFCInvalidSource(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: staging
    path: envs/staging
    rfc:
      source: jira
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid rfc source")
	}
}

func TestValidate_DeploymentWindow_InvalidDay(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: staging
    path: envs/staging
    deployment_window:
      days: [monday, funday]
      start: "09:00"
      end: "17:00"
      timezone: UTC
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid day name")
	}
}

func TestValidate_DeploymentWindow_InvalidTimeFormat(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: staging
    path: envs/staging
    deployment_window:
      days: [monday]
      start: "9am"
      end: "17:00"
      timezone: UTC
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid time format")
	}
}

func TestValidate_DeploymentWindow_InvalidTimezone(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: staging
    path: envs/staging
    deployment_window:
      days: [monday]
      start: "09:00"
      end: "17:00"
      timezone: Fake/Zone
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid timezone")
	}
}
