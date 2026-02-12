//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/justinholmes/grunter/internal/changes"
	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/deps"
	"github.com/justinholmes/grunter/internal/gitlab"
	"github.com/justinholmes/grunter/internal/plan"
	"github.com/justinholmes/grunter/internal/runner"
)

// TestE2E_GitLabDriftIntegration is a full end-to-end test that:
// 1. Creates a local repo with terragrunt units (VPC, RDS, API service)
// 2. Applies initial state with real terraform
// 3. Pushes to a GitLab project and creates a merge request with changes
// 4. Runs change detection and dependency resolution
// 5. Runs terragrunt plan on changed units
// 6. Posts plan results as formatted MR comments via GitLab API
// 7. Verifies comments appear on the MR
// 8. Introduces drift (modifies code after apply)
// 9. Runs drift detection and posts a drift report to the MR
// 10. Verifies drift report appears on the MR
func TestE2E_GitLabDriftIntegration(t *testing.T) {
	RequireBinary(t, "terragrunt")
	RequireBinary(t, "tofu")
	RequireBinary(t, "git")

	gitlabURL := os.Getenv("GRUNTER_TEST_GITLAB_URL")
	gitlabToken := os.Getenv("GRUNTER_TEST_GITLAB_TOKEN")
	if gitlabURL == "" || gitlabToken == "" {
		t.Skip("skipping: GRUNTER_TEST_GITLAB_URL and GRUNTER_TEST_GITLAB_TOKEN must be set")
	}

	tmp := t.TempDir()

	// ===== Phase 1: Build a realistic multi-unit repo =====
	t.Log("=== Phase 1: Build repo with terragrunt units ===")

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
	os.WriteFile(filepath.Join(tmp, ".grunter", "config.yml"), []byte(`
deploy_branch: main
tf_binary: tofu
tg_binary: terragrunt
ignore:
  - "**/README.md"
  - "**/.terraform.lock.hcl"
`), 0644)

	os.WriteFile(filepath.Join(tmp, ".gitignore"), []byte(`
.terraform/
.terragrunt-cache/
*.tfstate
*.tfstate.backup
.terraform.lock.hcl
`), 0644)

	// VPC unit (no deps)
	vpcTF := `
terraform {
  required_providers {
    null = { source = "hashicorp/null", version = "~> 3.0" }
  }
  backend "local" {}
}

resource "null_resource" "vpc" {
  triggers = {
    name = "production-vpc"
    cidr = "10.0.0.0/16"
  }
}

output "vpc_id" { value = null_resource.vpc.id }
`
	vpcDir := CreateTerragruntUnit(t, tmp, "networking/vpc", `# no deps`, vpcTF)

	// RDS unit (depends on VPC)
	rdsTF := `
terraform {
  required_providers {
    null = { source = "hashicorp/null", version = "~> 3.0" }
  }
  backend "local" {}
}

resource "null_resource" "rds" {
  triggers = {
    engine         = "postgres"
    engine_version = "15.4"
    instance_class = "db.r6g.xlarge"
  }
}

output "rds_endpoint" { value = "rds.example.com" }
`
	rdsHCL := `
dependency "vpc" {
  config_path = "../../networking/vpc"
}
`
	rdsDir := CreateTerragruntUnit(t, tmp, "data/rds", rdsHCL, rdsTF)

	// API service (depends on VPC and RDS) - initial version
	apiTFv1 := `
terraform {
  required_providers {
    null = { source = "hashicorp/null", version = "~> 3.0" }
  }
  backend "local" {}
}

resource "null_resource" "ecs_service" {
  triggers = {
    name          = "api"
    desired_count = "3"
    image         = "registry.example.com/api:v1.0.0"
    cpu           = "512"
    memory        = "1024"
  }
}

output "service_url" { value = "https://api.example.com" }
`
	apiHCLv1 := `
dependency "vpc" {
  config_path = "../../networking/vpc"
}
dependency "rds" {
  config_path = "../../data/rds"
}
`
	apiDir := CreateTerragruntUnit(t, tmp, "services/api", apiHCLv1, apiTFv1)

	// ===== Phase 2: Init git, apply initial state =====
	t.Log("=== Phase 2: Apply initial state ===")
	InitGitRepoWithMain(t, tmp)

	cfg, err := config.Load(filepath.Join(tmp, ".grunter", "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	r := runner.New(cfg.TFBinary, cfg.TGBinary)

	// Apply in dependency order
	for _, unitDir := range []string{vpcDir, rdsDir, apiDir} {
		relPath, _ := filepath.Rel(tmp, unitDir)
		t.Logf("  Applying %s...", relPath)
		result, err := r.Run(unitDir, "apply", 5*time.Minute)
		if err != nil {
			t.Fatalf("apply error for %s: %v", unitDir, err)
		}
		if !result.Success {
			t.Fatalf("apply failed for %s:\n%s", unitDir, result.CombinedOutput())
		}
	}

	baseSHA := GitGetHEAD(t, tmp)
	t.Logf("Base SHA (initial apply): %s", baseSHA)

	// ===== Phase 3: Make changes on a feature branch =====
	t.Log("=== Phase 3: Create feature branch with changes ===")

	// Update API service: bump image, add env var
	apiTFv2 := `
terraform {
  required_providers {
    null = { source = "hashicorp/null", version = "~> 3.0" }
  }
  backend "local" {}
}

resource "null_resource" "ecs_service" {
  triggers = {
    name          = "api"
    desired_count = "3"
    image         = "registry.example.com/api:v2.0.0"
    cpu           = "512"
    memory        = "1024"
  }
}

resource "null_resource" "task_env" {
  triggers = {
    REDIS_URL = "redis://redis.example.com:6379"
  }
}

output "service_url" { value = "https://api.example.com" }
`
	os.WriteFile(filepath.Join(apiDir, "main.tf"), []byte(apiTFv2), 0644)

	// Also bump RDS engine version (simulates an upgrade)
	rdsTFv2 := `
terraform {
  required_providers {
    null = { source = "hashicorp/null", version = "~> 3.0" }
  }
  backend "local" {}
}

resource "null_resource" "rds" {
  triggers = {
    engine         = "postgres"
    engine_version = "16.1"
    instance_class = "db.r6g.xlarge"
  }
}

output "rds_endpoint" { value = "rds.example.com" }
`
	os.WriteFile(filepath.Join(rdsDir, "main.tf"), []byte(rdsTFv2), 0644)

	headSHA := GitCommitAll(t, tmp, "Upgrade API to v2.0.0 and RDS to PostgreSQL 16.1")
	t.Logf("Head SHA (feature commit): %s", headSHA)

	// ===== Phase 4: Push to GitLab and create MR =====
	t.Log("=== Phase 4: Push to GitLab and create MR ===")

	projectID := createGitLabProject(t, gitlabURL, gitlabToken)
	t.Logf("GitLab project ID: %d", projectID)

	// Add GitLab remote and push
	gitCmd(t, tmp, "remote", "add", "origin",
		fmt.Sprintf("%s/root/grunter-drift-test.git", strings.Replace(gitlabURL, "://", "://root:testpassword123@", 1)))
	gitCmd(t, tmp, "push", "-u", "origin", "main")

	// Create feature branch in git and push
	gitCmd(t, tmp, "checkout", "-b", "feature/api-upgrade")
	// The commit is already on current branch, push it
	gitCmd(t, tmp, "push", "-u", "origin", "feature/api-upgrade")

	// Create MR via API
	mrIID := createGitLabMR(t, gitlabURL, gitlabToken, projectID,
		"feature/api-upgrade", "main", "Upgrade API v2.0.0 and PostgreSQL 16.1")
	t.Logf("MR IID: %d", mrIID)

	// ===== Phase 5: Run change detection and dependency graph =====
	t.Log("=== Phase 5: Detect changes and build execution plan ===")

	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	detector := changes.NewDetector(cfg.Ignore)
	detected, err := detector.Detect(baseSHA, headSHA)
	if err != nil {
		t.Fatalf("change detection failed: %v", err)
	}

	t.Logf("Detected %d changed units:", len(detected))
	for _, c := range detected {
		t.Logf("  %s (%s)", c.UnitPath, c.Type)
	}

	if len(detected) != 2 {
		t.Fatalf("expected 2 changed units (data/rds, services/api), got %d", len(detected))
	}

	unitPaths := make([]string, 0, len(detected))
	for _, c := range detected {
		unitPaths = append(unitPaths, c.UnitPath)
	}

	graph, err := deps.BuildGraph(unitPaths)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}

	sorted, err := graph.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	t.Logf("Execution order: %v", sorted)

	// ===== Phase 6: Run terragrunt plan on changed units =====
	t.Log("=== Phase 6: Run plans on changed units ===")

	var planResults []plan.UnitResult
	for _, unitPath := range sorted {
		t.Logf("  Planning %s...", unitPath)
		result, err := r.Run(unitPath, "plan", 5*time.Minute)
		if err != nil {
			t.Fatalf("plan error for %s: %v", unitPath, err)
		}

		combined := result.CombinedOutput()
		summary := runner.ParsePlanOutput(combined)
		t.Logf("    Summary: %s (success=%v)", summary.String(), result.Success)

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

	// ===== Phase 7: Post plan comments to GitLab MR =====
	t.Log("=== Phase 7: Post plan comments to MR ===")

	client := gitlab.NewClient(gitlabURL, gitlabToken)
	projStr := strconv.Itoa(projectID)
	mrStr := strconv.Itoa(mrIID)

	for _, pr := range planResults {
		formatted := plan.FormatComment(pr.Path, "plan", pr.Output)
		err := client.PostOrUpdateComment(projStr, mrStr, pr.Path, formatted)
		if err != nil {
			t.Fatalf("posting comment for %s: %v", pr.Path, err)
		}
		t.Logf("  Posted plan comment for %s", pr.Path)
	}

	// ===== Phase 8: Verify plan comments on MR =====
	t.Log("=== Phase 8: Verify plan comments ===")

	notes := listMRNotes(t, gitlabURL, gitlabToken, projectID, mrIID)
	for _, unitPath := range sorted {
		marker := "<!-- grunter:" + unitPath + " -->"
		found := false
		for _, n := range notes {
			if strings.Contains(n.Body, marker) {
				found = true
				// Verify the comment has plan summary info
				if !strings.Contains(n.Body, "to add") && !strings.Contains(n.Body, "to change") && !strings.Contains(n.Body, "to destroy") {
					t.Errorf("plan comment for %s missing summary", unitPath)
				}
				break
			}
		}
		if !found {
			t.Errorf("no plan comment found on MR for unit %s", unitPath)
		}
	}
	t.Logf("All %d plan comments verified on MR", len(sorted))

	// ===== Phase 9: Apply the changes, then introduce drift =====
	t.Log("=== Phase 9: Apply changes then introduce drift ===")

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

	// Verify clean state after apply
	for _, unitPath := range sorted {
		result, err := r.Run(unitPath, "plan", 5*time.Minute)
		if err != nil {
			t.Fatalf("post-apply plan error for %s: %v", unitPath, err)
		}
		summary := runner.ParsePlanOutput(result.CombinedOutput())
		if summary.HasChanges() {
			t.Fatalf("unexpected drift in %s right after apply: %s", unitPath, summary.String())
		}
	}
	t.Log("  State is clean after apply")

	// Now introduce drift: modify the API service code WITHOUT applying
	// This simulates someone making a manual change or state becoming stale
	apiTFDrifted := `
terraform {
  required_providers {
    null = { source = "hashicorp/null", version = "~> 3.0" }
  }
  backend "local" {}
}

resource "null_resource" "ecs_service" {
  triggers = {
    name          = "api"
    desired_count = "5"
    image         = "registry.example.com/api:v3.0.0-hotfix"
    cpu           = "1024"
    memory        = "2048"
  }
}

resource "null_resource" "task_env" {
  triggers = {
    REDIS_URL = "redis://redis.example.com:6379"
    NEW_VAR   = "introduced-drift"
  }
}

output "service_url" { value = "https://api.example.com" }
`
	os.WriteFile(filepath.Join(apiDir, "main.tf"), []byte(apiTFDrifted), 0644)
	t.Log("  Introduced drift in services/api (image v3.0.0-hotfix, scaled to 5, new env var)")

	// ===== Phase 10: Run drift detection =====
	t.Log("=== Phase 10: Run drift detection ===")

	allUnits := []string{vpcDir, rdsDir, apiDir}
	var driftResults []plan.UnitResult

	for _, unitDir := range allUnits {
		relPath, _ := filepath.Rel(tmp, unitDir)
		result, err := r.Run(unitDir, "plan", 5*time.Minute)
		if err != nil {
			t.Fatalf("drift plan error for %s: %v", relPath, err)
		}

		combined := result.CombinedOutput()
		summary := runner.ParsePlanOutput(combined)

		if summary.HasChanges() {
			t.Logf("  DRIFT in %s: %s", relPath, summary.String())
			driftResults = append(driftResults, plan.UnitResult{
				Path:    relPath,
				Status:  plan.StatusDrift,
				Output:  combined,
				Summary: summary,
			})
		} else {
			t.Logf("  Clean: %s", relPath)
		}
	}

	// Only API should have drift
	if len(driftResults) != 1 {
		t.Fatalf("expected 1 unit with drift, got %d", len(driftResults))
	}
	if driftResults[0].Path != "services/api" {
		t.Errorf("expected drift in services/api, got %s", driftResults[0].Path)
	}

	// Verify the drift includes meaningful changes
	driftSummary := driftResults[0].Summary
	if driftSummary.ToDestroy < 1 && driftSummary.ToChange < 1 && driftSummary.ToAdd < 1 {
		t.Error("drift summary shows no resource changes")
	}

	// ===== Phase 11: Post drift report to MR =====
	t.Log("=== Phase 11: Post drift report to MR ===")

	driftReport := plan.FormatDriftReport(driftResults)
	t.Logf("Drift report (%d bytes):\n%s", len(driftReport), driftReport[:minInt(len(driftReport), 500)])

	err = client.PostOrUpdateComment(projStr, mrStr, "drift-report", driftReport)
	if err != nil {
		t.Fatalf("posting drift report: %v", err)
	}
	t.Log("  Posted drift report to MR")

	// ===== Phase 12: Verify drift report on MR =====
	t.Log("=== Phase 12: Verify drift report on MR ===")

	notes = listMRNotes(t, gitlabURL, gitlabToken, projectID, mrIID)
	driftMarker := "<!-- grunter:drift-report -->"
	driftCommentFound := false
	for _, n := range notes {
		if strings.Contains(n.Body, driftMarker) {
			driftCommentFound = true
			if !strings.Contains(n.Body, "Drift Detection Report") {
				t.Error("drift comment missing report header")
			}
			if !strings.Contains(n.Body, "services/api") {
				t.Error("drift comment missing drifted unit path")
			}
			if !strings.Contains(n.Body, "**1** unit(s)") {
				t.Error("drift comment missing unit count")
			}
			break
		}
	}
	if !driftCommentFound {
		t.Error("drift report comment not found on MR")
	}

	// Count total grunter comments on the MR
	grunterComments := 0
	for _, n := range notes {
		if strings.Contains(n.Body, "<!-- grunter:") {
			grunterComments++
		}
	}
	// Should have: 2 plan comments (rds, api) + 1 drift report = 3
	if grunterComments != 3 {
		t.Errorf("expected 3 grunter comments on MR (2 plans + 1 drift), got %d", grunterComments)
	}

	t.Log("=== E2E GitLab Drift Integration test passed! ===")
}

// Helper functions for GitLab API interactions

type mrNote struct {
	ID   int    `json:"id"`
	Body string `json:"body"`
}

func listMRNotes(t *testing.T, gitlabURL, token string, projectID, mrIID int) []mrNote {
	t.Helper()
	url := fmt.Sprintf("%s/api/v4/projects/%d/merge_requests/%d/notes?per_page=100",
		gitlabURL, projectID, mrIID)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("PRIVATE-TOKEN", token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("listing MR notes: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var notes []mrNote
	if err := json.Unmarshal(body, &notes); err != nil {
		t.Fatalf("parsing MR notes: %v\nbody: %s", err, body)
	}
	return notes
}

func createGitLabProject(t *testing.T, gitlabURL, token string) int {
	t.Helper()

	// Try to find existing project first
	searchURL := fmt.Sprintf("%s/api/v4/projects?search=grunter-drift-test&owned=true", gitlabURL)
	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("PRIVATE-TOKEN", token)

	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var projects []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		json.Unmarshal(body, &projects)
		for _, p := range projects {
			if p.Name == "grunter-drift-test" {
				// Delete and recreate to ensure clean state
				deleteURL := fmt.Sprintf("%s/api/v4/projects/%d", gitlabURL, p.ID)
				delReq, _ := http.NewRequest("DELETE", deleteURL, nil)
				delReq.Header.Set("PRIVATE-TOKEN", token)
				delResp, _ := http.DefaultClient.Do(delReq)
				if delResp != nil {
					delResp.Body.Close()
				}
				// Wait briefly for deletion to process
				time.Sleep(2 * time.Second)
				break
			}
		}
	}

	// Create fresh project
	payload := map[string]interface{}{
		"name":       "grunter-drift-test",
		"visibility": "internal",
	}
	data, _ := json.Marshal(payload)

	req, _ = http.NewRequest("POST", gitlabURL+"/api/v4/projects", bytes.NewReader(data))
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Content-Type", "application/json")

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("parsing project response: %v\nbody: %s", err, body)
	}
	if result.ID == 0 {
		t.Fatalf("failed to create project, response: %s", body)
	}
	return result.ID
}

func createGitLabMR(t *testing.T, gitlabURL, token string, projectID int, sourceBranch, targetBranch, title string) int {
	t.Helper()

	payload := map[string]string{
		"source_branch": sourceBranch,
		"target_branch": targetBranch,
		"title":         title,
	}
	data, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/api/v4/projects/%d/merge_requests", gitlabURL, projectID)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("creating MR: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		IID int `json:"iid"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("parsing MR response: %v\nbody: %s", err, body)
	}
	if result.IID == 0 {
		t.Fatalf("failed to create MR, response: %s", body)
	}
	return result.IID
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
