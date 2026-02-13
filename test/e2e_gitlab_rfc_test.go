//go:build integration

package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestE2E_GitLabRFCCheck exercises the grunter rfc-check CLI command end-to-end:
// 1. Builds a realistic repo with Terragrunt/Terraform files and .grunter/config.yml
// 2. Pushes to a real GitLab instance, creates an MR
// 3. Sets up labels, issues, and issue-MR links
// 4. Runs grunter rfc-check as a subprocess and verifies exit codes + stderr output
//    for rejected, approved, emergency, no-config, and post-merge scenarios
func TestE2E_GitLabRFCCheck(t *testing.T) {
	RequireBinary(t, "git")

	gitlabURL := os.Getenv("GRUNTER_TEST_GITLAB_URL")
	gitlabToken := os.Getenv("GRUNTER_TEST_GITLAB_TOKEN")
	if gitlabURL == "" || gitlabToken == "" {
		t.Skip("skipping: GRUNTER_TEST_GITLAB_URL and GRUNTER_TEST_GITLAB_TOKEN must be set")
	}

	// ===== Phase 0: Build grunter binary =====
	t.Log("=== Phase 0: Build grunter binary ===")
	binDir := t.TempDir()
	grunterBin := filepath.Join(binDir, "grunter")
	buildCmd := exec.Command("go", "build", "-o", grunterBin, ".")
	buildCmd.Dir = filepath.Join(mustGetwd(t), "..")
	buildOut, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building grunter binary: %v\n%s", err, buildOut)
	}
	t.Logf("Built grunter binary: %s", grunterBin)

	// ===== Phase 1: Build a realistic repo =====
	t.Log("=== Phase 1: Build repo with terragrunt files ===")
	tmp := t.TempDir()

	// Root terragrunt.hcl with local backend
	os.WriteFile(filepath.Join(tmp, "terragrunt.hcl"), []byte(`
remote_state {
  backend = "local"
  config = {
    path = "${get_terragrunt_dir()}/terraform.tfstate"
  }
}
`), 0644)

	os.MkdirAll(filepath.Join(tmp, ".grunter"), 0755)
	os.WriteFile(filepath.Join(tmp, ".grunter", "config.yml"), []byte(`deploy_branch: main
tf_binary: tofu
tg_binary: terragrunt
ignore:
  - "**/README.md"
  - "**/.terraform.lock.hcl"
environments:
  - name: dev
    path: envs/dev
  - name: staging
    path: envs/staging
    rfc:
      source: gitlab
      approved_label: rfc-approved
      issue_approved_label: "status::approved"
      emergency_label: emergency
`), 0644)

	os.WriteFile(filepath.Join(tmp, ".gitignore"), []byte(`
.terraform/
.terragrunt-cache/
*.tfstate
*.tfstate.backup
.terraform.lock.hcl
`), 0644)

	// Create a staging VPC unit
	vpcTF := `
terraform {
  required_providers {
    null = { source = "hashicorp/null", version = "~> 3.0" }
  }
  backend "local" {}
}

resource "null_resource" "vpc" {
  triggers = {
    name = "staging-vpc"
    cidr = "10.1.0.0/16"
  }
}

output "vpc_id" { value = null_resource.vpc.id }
`
	CreateTerragruntUnit(t, tmp, "envs/staging/us-east-1/vpc", `# no deps`, vpcTF)

	// Init git on main branch
	InitGitRepoWithMain(t, tmp)

	// Create a feature branch with a change
	gitCmd(t, tmp, "checkout", "-b", "feature/rfc-test")
	os.WriteFile(filepath.Join(tmp, "envs", "staging", "us-east-1", "vpc", "main.tf"), []byte(`
terraform {
  required_providers {
    null = { source = "hashicorp/null", version = "~> 3.0" }
  }
  backend "local" {}
}

resource "null_resource" "vpc" {
  triggers = {
    name = "staging-vpc"
    cidr = "10.1.0.0/16"
    tag  = "updated"
  }
}

output "vpc_id" { value = null_resource.vpc.id }
`), 0644)
	GitCommitAll(t, tmp, "Update staging VPC")

	// ===== Phase 2: Push to GitLab and create MR =====
	t.Log("=== Phase 2: Push to GitLab and create MR ===")

	projectName := fmt.Sprintf("grunter-rfc-e2e-%d", time.Now().UnixNano())
	env := &GitLabTestEnv{URL: gitlabURL, Token: gitlabToken}
	// Create project WITHOUT initialize_with_readme to avoid protected-branch issues on push
	projectID := createEmptyGitLabProject(t, gitlabURL, gitlabToken, projectName)
	t.Logf("GitLab project ID: %d", projectID)

	// Push our repo — set origin with credentials
	remoteURL := fmt.Sprintf("%s/root/%s.git",
		strings.Replace(gitlabURL, "://", "://root:testpassword123@", 1), projectName)
	gitCmd(t, tmp, "remote", "add", "origin", remoteURL)
	gitCmd(t, tmp, "push", "-u", "origin", "main")
	gitCmd(t, tmp, "push", "-u", "origin", "feature/rfc-test")

	mrIID := createGitLabMR(t, gitlabURL, gitlabToken, projectID,
		"feature/rfc-test", "main", "RFC E2E test MR")
	t.Logf("MR IID: %d", mrIID)

	// ===== Phase 3: Setup labels and issues =====
	t.Log("=== Phase 3: Create labels and linked issue ===")

	env.CreateLabel(t, projectID, "rfc-approved", "#0033cc")
	env.CreateLabel(t, projectID, "status::approved", "#5cb85c")
	env.CreateLabel(t, projectID, "emergency", "#d9534f")

	issueIID := env.CreateIssue(t, projectID, "RFC: staging VPC update", []string{"status::approved"})
	t.Logf("Issue IID: %d", issueIID)

	// Link issue to MR via description
	env.UpdateMRDescription(t, projectID, mrIID, fmt.Sprintf("Closes #%d", issueIID))

	projStr := strconv.Itoa(projectID)
	mrStr := strconv.Itoa(mrIID)

	// Helper to run grunter rfc-check with given env vars and flags
	runRFCCheck := func(envVars map[string]string, extraFlags ...string) (int, string) {
		t.Helper()
		args := []string{"rfc-check", "--config", filepath.Join(tmp, ".grunter", "config.yml")}
		args = append(args, extraFlags...)
		cmd := exec.Command(grunterBin, args...)
		cmd.Dir = tmp
		cmd.Env = []string{
			"HOME=" + os.Getenv("HOME"),
			"PATH=" + os.Getenv("PATH"),
		}
		for k, v := range envVars {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		out, err := cmd.CombinedOutput()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				t.Fatalf("running grunter rfc-check: %v\n%s", err, out)
			}
		}
		return exitCode, string(out)
	}

	baseEnvVars := map[string]string{
		"GITLAB_TOKEN":        gitlabToken,
		"CI_PROJECT_ID":       projStr,
		"CI_MERGE_REQUEST_IID": mrStr,
		"CI_SERVER_URL":       gitlabURL,
	}

	// ===== Phase 4: Rejected — MR missing rfc-approved label =====
	t.Log("=== Phase 4: Rejected — missing MR label ===")

	// Make sure MR has no labels
	env.SetMRLabels(t, projectID, mrIID, []string{})

	exitCode, output := runRFCCheck(baseEnvVars, "--env", "staging")
	t.Logf("Exit code: %d, output:\n%s", exitCode, output)

	if exitCode != 1 {
		t.Errorf("expected exit code 1 for rejected RFC, got %d", exitCode)
	}
	if !strings.Contains(output, "missing label") {
		t.Errorf("expected stderr to contain 'missing label', got:\n%s", output)
	}

	// ===== Phase 5: Approved — full flow =====
	t.Log("=== Phase 5: Approved — MR label + linked issue ===")

	env.SetMRLabels(t, projectID, mrIID, []string{"rfc-approved"})

	exitCode, output = runRFCCheck(baseEnvVars, "--env", "staging")
	t.Logf("Exit code: %d, output:\n%s", exitCode, output)

	if exitCode != 0 {
		t.Errorf("expected exit code 0 for approved RFC, got %d", exitCode)
	}
	if !strings.Contains(output, "RFC check passed") {
		t.Errorf("expected stderr to contain 'RFC check passed', got:\n%s", output)
	}

	// ===== Phase 6: Emergency =====
	t.Log("=== Phase 6: Emergency label ===")

	env.SetMRLabels(t, projectID, mrIID, []string{"rfc-approved", "emergency"})

	exitCode, output = runRFCCheck(baseEnvVars, "--env", "staging")
	t.Logf("Exit code: %d, output:\n%s", exitCode, output)

	if exitCode != 0 {
		t.Errorf("expected exit code 0 for emergency RFC, got %d", exitCode)
	}
	if !strings.Contains(output, "Emergency RFC detected") {
		t.Errorf("expected stderr to contain 'Emergency RFC detected', got:\n%s", output)
	}

	// ===== Phase 7: No RFC config (dev env) =====
	t.Log("=== Phase 7: No RFC config for dev ===")

	exitCode, output = runRFCCheck(baseEnvVars, "--env", "dev")
	t.Logf("Exit code: %d, output:\n%s", exitCode, output)

	if exitCode != 0 {
		t.Errorf("expected exit code 0 for env without RFC config, got %d", exitCode)
	}
	if !strings.Contains(output, "No RFC config") {
		t.Errorf("expected stderr to contain 'No RFC config', got:\n%s", output)
	}

	// ===== Phase 8: Post-merge commit SHA =====
	t.Log("=== Phase 8: Post-merge — lookup MR from commit SHA ===")

	// Restore rfc-approved for merge
	env.SetMRLabels(t, projectID, mrIID, []string{"rfc-approved"})

	mergeCommitSHA := env.MergeMR(t, projectID, mrIID)
	t.Logf("Merge commit SHA: %s", mergeCommitSHA)

	postMergeVars := map[string]string{
		"GITLAB_TOKEN":  gitlabToken,
		"CI_PROJECT_ID": projStr,
		"CI_SERVER_URL": gitlabURL,
		"CI_COMMIT_SHA": mergeCommitSHA,
		// CI_MERGE_REQUEST_IID intentionally omitted
	}

	exitCode, output = runRFCCheck(postMergeVars, "--env", "staging")
	t.Logf("Exit code: %d, output:\n%s", exitCode, output)

	if exitCode != 0 {
		t.Errorf("expected exit code 0 for post-merge RFC check, got %d", exitCode)
	}
	if !strings.Contains(output, "RFC check passed") {
		t.Errorf("expected stderr to contain 'RFC check passed', got:\n%s", output)
	}

	t.Log("=== E2E GitLab RFC Check test passed! ===")
}

// mustGetwd returns the current working directory or fails the test.
func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working directory: %v", err)
	}
	return wd
}

// createEmptyGitLabProject creates a GitLab project without initialize_with_readme
// so that the default branch is not protected and we can push freely.
func createEmptyGitLabProject(t *testing.T, gitlabURL, token, name string) int {
	t.Helper()

	env := &GitLabTestEnv{URL: gitlabURL, Token: token}

	var result struct {
		ID int `json:"id"`
	}
	err := env.APIRequest(t, "POST", "/api/v4/projects", map[string]interface{}{
		"name":       name,
		"visibility": "internal",
	}, &result)
	if err != nil {
		t.Fatalf("creating project %q: %v", name, err)
	}
	if result.ID == 0 {
		t.Fatalf("created project %q has ID 0", name)
	}
	return result.ID
}
