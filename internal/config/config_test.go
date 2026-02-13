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

func TestLoad_ParsesRFCConfig_Custom(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: prod
    path: envs/prod
    rfc:
      source: custom
      url: "https://change-mgmt.example.com/api/v1/changes/{{change_id}}"
      method: GET
      change_number_var: RFC_CHANGE_ID
      auth_type: bearer
      auth_token_var: CHANGE_API_TOKEN
      response_type: object
      response_path: data
      approved_field: status
      approved_value: approved
      emergency_field: priority
      emergency_value: P1
      identifier_field: ticket_number
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
	if env.RFC.Source != RFCSourceCustom {
		t.Errorf("expected source=custom, got %s", env.RFC.Source)
	}
	if env.RFC.URL != "https://change-mgmt.example.com/api/v1/changes/{{change_id}}" {
		t.Errorf("unexpected url: %s", env.RFC.URL)
	}
	if env.RFC.AuthType != "bearer" {
		t.Errorf("expected auth_type=bearer, got %s", env.RFC.AuthType)
	}
	if env.RFC.ApprovedField != "status" {
		t.Errorf("expected approved_field=status, got %s", env.RFC.ApprovedField)
	}
	if env.RFC.ApprovedValue != "approved" {
		t.Errorf("expected approved_value=approved, got %s", env.RFC.ApprovedValue)
	}
	if env.RFC.EmergencyField != "priority" {
		t.Errorf("expected emergency_field=priority, got %s", env.RFC.EmergencyField)
	}
	if env.RFC.IdentifierField != "ticket_number" {
		t.Errorf("expected identifier_field=ticket_number, got %s", env.RFC.IdentifierField)
	}
}

func TestLoad_ParsesDeploymentWindow_Custom(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: prod
    path: envs/prod
    deployment_window:
      source: custom
      url: "https://deploy-gate.example.com/api/v1/windows/current"
      auth_type: bearer
      auth_token_var: DEPLOY_GATE_TOKEN
      response_path: data
      allowed_field: is_open
      message_field: reason
      next_window_field: next_open_at
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
	if env.DeploymentWindow.Source != DeploymentWindowCustom {
		t.Errorf("expected source=custom, got %s", env.DeploymentWindow.Source)
	}
	if env.DeploymentWindow.URL != "https://deploy-gate.example.com/api/v1/windows/current" {
		t.Errorf("unexpected url: %s", env.DeploymentWindow.URL)
	}
	if env.DeploymentWindow.AllowedField != "is_open" {
		t.Errorf("expected allowed_field=is_open, got %s", env.DeploymentWindow.AllowedField)
	}
	if env.DeploymentWindow.MessageField != "reason" {
		t.Errorf("expected message_field=reason, got %s", env.DeploymentWindow.MessageField)
	}
	if env.DeploymentWindow.NextWindowField != "next_open_at" {
		t.Errorf("expected next_window_field=next_open_at, got %s", env.DeploymentWindow.NextWindowField)
	}
}

func TestValidate_RFCCustom_MissingURL(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: prod
    path: envs/prod
    rfc:
      source: custom
      approved_field: status
      approved_value: approved
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing url")
	}
}

func TestValidate_RFCCustom_MissingApprovedField(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: prod
    path: envs/prod
    rfc:
      source: custom
      url: "https://example.com/api"
      approved_value: approved
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing approved_field")
	}
}

func TestValidate_RFCCustom_InvalidAuthType(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: prod
    path: envs/prod
    rfc:
      source: custom
      url: "https://example.com/api"
      approved_field: status
      approved_value: approved
      auth_type: oauth2
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid auth_type")
	}
}

func TestValidate_RFCCustom_BasicAuthMissingUsername(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: prod
    path: envs/prod
    rfc:
      source: custom
      url: "https://example.com/api"
      approved_field: status
      approved_value: approved
      auth_type: basic
      auth_token_var: API_PASS
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing auth_username_var with basic auth")
	}
}

func TestValidate_RFCCustom_HeaderAuthMissingHeaderName(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: prod
    path: envs/prod
    rfc:
      source: custom
      url: "https://example.com/api"
      approved_field: status
      approved_value: approved
      auth_type: header
      auth_token_var: API_KEY
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing auth_header_name with header auth")
	}
}

func TestValidate_DeploymentWindowCustom_MissingURL(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: prod
    path: envs/prod
    deployment_window:
      source: custom
      allowed_field: is_open
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing url in custom deployment window")
	}
}

func TestValidate_DeploymentWindowCustom_MissingAllowedField(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: prod
    path: envs/prod
    deployment_window:
      source: custom
      url: "https://deploy-gate.example.com/api/v1/windows/current"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing allowed_field in custom deployment window")
	}
}

func TestValidate_DeploymentWindow_ScheduleBackwardCompat(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	// No source field — should default to schedule and still work
	content := `
environments:
  - name: staging
    path: envs/staging
    deployment_window:
      days: [monday, friday]
      start: "08:00"
      end: "16:00"
      timezone: UTC
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("expected no error for schedule window without source, got: %v", err)
	}

	env := cfg.GetEnvironment("staging")
	if !env.HasDeploymentWindow() {
		t.Error("expected HasDeploymentWindow to be true")
	}
}

func TestValidate_DeploymentWindow_InvalidSource(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: prod
    path: envs/prod
    deployment_window:
      source: magic
      url: "https://example.com"
      allowed_field: open
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid deployment_window source")
	}
}

func TestLoad_ParsesRFCConfig_CustomIDToken(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: prod
    path: envs/prod
    rfc:
      source: custom
      url: "https://change-mgmt.example.com/api/v1/changes/{{change_id}}"
      auth_type: id_token
      auth_token_var: VAULT_ID_TOKEN
      auth_token_aud: https://vault.example.com
      approved_field: status
      approved_value: approved
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
	if env.RFC.AuthType != "id_token" {
		t.Errorf("expected auth_type=id_token, got %s", env.RFC.AuthType)
	}
	if env.RFC.AuthTokenVar != "VAULT_ID_TOKEN" {
		t.Errorf("expected auth_token_var=VAULT_ID_TOKEN, got %s", env.RFC.AuthTokenVar)
	}
	if env.RFC.AuthTokenAud != "https://vault.example.com" {
		t.Errorf("expected auth_token_aud=https://vault.example.com, got %s", env.RFC.AuthTokenAud)
	}
}

func TestValidate_RFCCustom_IDTokenMissingTokenVar(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: prod
    path: envs/prod
    rfc:
      source: custom
      url: "https://example.com/api"
      approved_field: status
      approved_value: approved
      auth_type: id_token
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing auth_token_var with id_token auth")
	}
}

func TestLoad_ParsesMaxDestroy(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: dev
    path: envs/dev
  - name: staging
    path: envs/staging
    max_destroy: 0
  - name: prod
    path: envs/prod
    max_destroy: 5
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	// dev: omitted → nil
	dev := cfg.GetEnvironment("dev")
	if dev.MaxDestroy != nil {
		t.Error("expected dev MaxDestroy to be nil")
	}
	if dev.HasDestroyProtection() {
		t.Error("expected dev HasDestroyProtection to be false")
	}

	// staging: 0 → pointer to 0
	staging := cfg.GetEnvironment("staging")
	if staging.MaxDestroy == nil {
		t.Fatal("expected staging MaxDestroy to be non-nil")
	}
	if *staging.MaxDestroy != 0 {
		t.Errorf("expected staging MaxDestroy=0, got %d", *staging.MaxDestroy)
	}
	if !staging.HasDestroyProtection() {
		t.Error("expected staging HasDestroyProtection to be true")
	}

	// prod: 5
	prod := cfg.GetEnvironment("prod")
	if prod.MaxDestroy == nil {
		t.Fatal("expected prod MaxDestroy to be non-nil")
	}
	if *prod.MaxDestroy != 5 {
		t.Errorf("expected prod MaxDestroy=5, got %d", *prod.MaxDestroy)
	}
}

func TestValidate_MaxDestroy_Negative(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")

	content := `
environments:
  - name: dev
    path: envs/dev
    max_destroy: -1
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for negative max_destroy")
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
