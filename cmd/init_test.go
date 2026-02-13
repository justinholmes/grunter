package cmd

import (
	"strings"
	"testing"

	"github.com/justinholmes/grunter/internal/config"
)

func TestCollectIDTokens_NoIDTokenAuth(t *testing.T) {
	env := config.Environment{
		Name: "prod",
		Path: "envs/prod",
		RFC: &config.RFCConfig{
			Source:        config.RFCSourceCustom,
			URL:           "https://example.com/api",
			AuthType:      "bearer",
			AuthTokenVar:  "API_TOKEN",
			ApprovedField: "status",
			ApprovedValue: "approved",
		},
	}

	tokens := collectIDTokens(env)
	if len(tokens) != 0 {
		t.Errorf("expected no ID tokens for bearer auth, got %d", len(tokens))
	}
}

func TestCollectIDTokens_RFCIDToken(t *testing.T) {
	env := config.Environment{
		Name: "prod",
		Path: "envs/prod",
		RFC: &config.RFCConfig{
			Source:        config.RFCSourceCustom,
			URL:           "https://example.com/api",
			AuthType:      "id_token",
			AuthTokenVar:  "VAULT_ID_TOKEN",
			AuthTokenAud:  "https://vault.example.com",
			ApprovedField: "status",
			ApprovedValue: "approved",
		},
	}

	tokens := collectIDTokens(env)
	if len(tokens) != 1 {
		t.Fatalf("expected 1 ID token, got %d", len(tokens))
	}
	if tokens[0].VarName != "VAULT_ID_TOKEN" {
		t.Errorf("expected var VAULT_ID_TOKEN, got %s", tokens[0].VarName)
	}
	if tokens[0].Audience != "https://vault.example.com" {
		t.Errorf("expected aud https://vault.example.com, got %s", tokens[0].Audience)
	}
}

func TestCollectIDTokens_DefaultAudience(t *testing.T) {
	env := config.Environment{
		Name: "prod",
		Path: "envs/prod",
		RFC: &config.RFCConfig{
			Source:        config.RFCSourceCustom,
			URL:           "https://example.com/api",
			AuthType:      "id_token",
			AuthTokenVar:  "MY_TOKEN",
			ApprovedField: "status",
			ApprovedValue: "approved",
		},
	}

	tokens := collectIDTokens(env)
	if len(tokens) != 1 {
		t.Fatalf("expected 1 ID token, got %d", len(tokens))
	}
	if tokens[0].Audience != "${CI_SERVER_URL}" {
		t.Errorf("expected default aud ${CI_SERVER_URL}, got %s", tokens[0].Audience)
	}
}

func TestCollectIDTokens_BothRFCAndWindow(t *testing.T) {
	env := config.Environment{
		Name: "prod",
		Path: "envs/prod",
		RFC: &config.RFCConfig{
			Source:        config.RFCSourceCustom,
			URL:           "https://example.com/api",
			AuthType:      "id_token",
			AuthTokenVar:  "RFC_TOKEN",
			AuthTokenAud:  "https://rfc.example.com",
			ApprovedField: "status",
			ApprovedValue: "approved",
		},
		DeploymentWindow: &config.DeploymentWindow{
			Source:       config.DeploymentWindowCustom,
			URL:          "https://deploy.example.com/api",
			AuthType:     "id_token",
			AuthTokenVar: "DEPLOY_TOKEN",
			AuthTokenAud: "https://deploy.example.com",
			AllowedField: "is_open",
		},
	}

	tokens := collectIDTokens(env)
	if len(tokens) != 2 {
		t.Fatalf("expected 2 ID tokens, got %d", len(tokens))
	}
	if tokens[0].VarName != "RFC_TOKEN" {
		t.Errorf("expected first token RFC_TOKEN, got %s", tokens[0].VarName)
	}
	if tokens[1].VarName != "DEPLOY_TOKEN" {
		t.Errorf("expected second token DEPLOY_TOKEN, got %s", tokens[1].VarName)
	}
}

func TestCollectIDTokens_DeduplicatesSameVar(t *testing.T) {
	env := config.Environment{
		Name: "prod",
		Path: "envs/prod",
		RFC: &config.RFCConfig{
			Source:        config.RFCSourceCustom,
			URL:           "https://example.com/api",
			AuthType:      "id_token",
			AuthTokenVar:  "SHARED_TOKEN",
			AuthTokenAud:  "https://example.com",
			ApprovedField: "status",
			ApprovedValue: "approved",
		},
		DeploymentWindow: &config.DeploymentWindow{
			Source:       config.DeploymentWindowCustom,
			URL:          "https://example.com/window",
			AuthType:     "id_token",
			AuthTokenVar: "SHARED_TOKEN",
			AuthTokenAud: "https://example.com",
			AllowedField: "is_open",
		},
	}

	tokens := collectIDTokens(env)
	if len(tokens) != 1 {
		t.Fatalf("expected 1 deduplicated ID token, got %d", len(tokens))
	}
}

func TestWriteIDTokens_EmitsYAML(t *testing.T) {
	env := config.Environment{
		Name: "prod",
		Path: "envs/prod",
		RFC: &config.RFCConfig{
			Source:        config.RFCSourceCustom,
			URL:           "https://example.com/api",
			AuthType:      "id_token",
			AuthTokenVar:  "VAULT_TOKEN",
			AuthTokenAud:  "https://vault.example.com",
			ApprovedField: "status",
			ApprovedValue: "approved",
		},
	}

	var b strings.Builder
	writeIDTokens(&b, env)
	output := b.String()

	if !strings.Contains(output, "id_tokens:") {
		t.Error("expected id_tokens: block")
	}
	if !strings.Contains(output, "VAULT_TOKEN:") {
		t.Error("expected VAULT_TOKEN: declaration")
	}
	if !strings.Contains(output, "aud: https://vault.example.com") {
		t.Error("expected aud: https://vault.example.com")
	}
}

func TestWriteIDTokens_NoOutput_WhenNoIDTokenAuth(t *testing.T) {
	env := config.Environment{
		Name: "prod",
		Path: "envs/prod",
		RFC: &config.RFCConfig{
			Source:        config.RFCSourceCustom,
			URL:           "https://example.com/api",
			AuthType:      "bearer",
			AuthTokenVar:  "API_TOKEN",
			ApprovedField: "status",
			ApprovedValue: "approved",
		},
	}

	var b strings.Builder
	writeIDTokens(&b, env)
	if b.Len() != 0 {
		t.Errorf("expected no output for bearer auth, got %q", b.String())
	}
}

func TestGenerateGitLabCI_IDTokensInApproveJob(t *testing.T) {
	cfg := &config.Config{
		DeployBranch: "main",
		TFBinary:     "opentofu",
		TGBinary:     "terragrunt",
		Environments: []config.Environment{
			{Name: "dev", Path: "envs/dev"},
			{
				Name: "prod",
				Path: "envs/prod",
				RFC: &config.RFCConfig{
					Source:        config.RFCSourceCustom,
					URL:           "https://example.com/api/{{change_id}}",
					AuthType:      "id_token",
					AuthTokenVar:  "OIDC_TOKEN",
					AuthTokenAud:  "https://change-api.example.com",
					ApprovedField: "status",
					ApprovedValue: "approved",
				},
			},
		},
	}

	output := generateGitLabCI(cfg, ".grunter/config.yml")

	// The approve:prod-deploy job should contain the id_tokens block
	if !strings.Contains(output, "id_tokens:") {
		t.Error("expected id_tokens block in generated CI YAML")
	}
	if !strings.Contains(output, "OIDC_TOKEN:") {
		t.Error("expected OIDC_TOKEN declaration in generated CI YAML")
	}
	if !strings.Contains(output, "aud: https://change-api.example.com") {
		t.Error("expected aud declaration in generated CI YAML")
	}
}
