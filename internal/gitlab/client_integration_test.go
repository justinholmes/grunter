//go:build integration

package gitlab

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

	testhelpers "github.com/justinholmes/grunter/test"
)

const gitlabImage = "docker.io/gitlab/gitlab-ce:latest"
const containerName = "grunter-test-gitlab"
const gitlabPort = "8929"

// gitlabEnv holds connection details for a running GitLab instance.
type gitlabEnv struct {
	URL       string
	Token     string
	ProjectID int
	MRIID     int
	podman    string
}

func setupGitLab(t *testing.T) *gitlabEnv {
	t.Helper()

	var env *gitlabEnv

	// Check if there's already a running test instance (for faster re-runs)
	if url := os.Getenv("GRUNTER_TEST_GITLAB_URL"); url != "" {
		env = &gitlabEnv{
			URL:   url,
			Token: os.Getenv("GRUNTER_TEST_GITLAB_TOKEN"),
		}
		if env.Token == "" {
			t.Fatal("GRUNTER_TEST_GITLAB_TOKEN must be set when GRUNTER_TEST_GITLAB_URL is set")
		}
		pid, _ := strconv.Atoi(os.Getenv("GRUNTER_TEST_GITLAB_PROJECT_ID"))
		env.ProjectID = pid
		mriid, _ := strconv.Atoi(os.Getenv("GRUNTER_TEST_GITLAB_MR_IID"))
		env.MRIID = mriid
	} else {
		podman := testhelpers.RequirePodman(t)

		env = &gitlabEnv{podman: podman}

		// Check if our container is already running
		out, err := exec.Command(podman, "ps", "--filter", "name="+containerName, "--format", "{{.ID}}").Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			t.Log("GitLab container already running, reusing")
		} else {
			t.Log("Starting GitLab CE container (this may take a few minutes on first run)...")
			startGitLabContainer(t, podman)
		}

		env.URL = fmt.Sprintf("http://localhost:%s", gitlabPort)

		t.Logf("GitLab URL: %s", env.URL)

		// Wait for GitLab to be ready
		waitForGitLab(t, env.URL, 10*time.Minute)

		// Create a personal access token via rails console
		env.Token = createRootToken(t, podman)
		t.Logf("Got root token: %s...", env.Token[:8])
	}

	// Create a test project if not provided
	if env.ProjectID == 0 {
		env.ProjectID = createTestProject(t, env)
		t.Logf("Created project ID: %d", env.ProjectID)
	}

	// Create a merge request if not provided
	if env.MRIID == 0 {
		env.MRIID = createTestMR(t, env)
		t.Logf("Created MR IID: %d", env.MRIID)
	}

	return env
}

func startGitLabContainer(t *testing.T, podman string) {
	t.Helper()

	// Remove any existing stopped container
	exec.Command(podman, "rm", "-f", containerName).Run()

	cmd := exec.Command(podman, "run", "-d",
		"--name", containerName,
		"-p", gitlabPort+":80",
		"-e", "GITLAB_ROOT_PASSWORD=testpassword123!",
		"-e", "GITLAB_SKIP_UNMIGRATED_DATA_CHECK=true",
		"-e", "GITLAB_OMNIBUS_CONFIG=gitlab_rails['initial_root_password']='testpassword123!'; gitlab_rails['monitoring_whitelist']=['0.0.0.0/0']; prometheus_monitoring['enable']=false; sidekiq['concurrency']=2; puma['worker_processes']=2;",
		gitlabImage,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("starting gitlab container: %v\n%s", err, out)
	}
	t.Logf("Container started: %s", strings.TrimSpace(string(out)))

	t.Cleanup(func() {
		// Don't remove on cleanup - let it persist for faster re-runs
		t.Logf("GitLab container %s left running for reuse. Run: podman rm -f %s to clean up.", containerName, containerName)
	})
}

func waitForGitLab(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	healthURL := baseURL + "/-/readiness?all=1"

	t.Logf("Waiting for GitLab to be ready at %s ...", healthURL)

	client := &http.Client{Timeout: 5 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				t.Log("GitLab is ready!")
				return
			}
			t.Logf("  GitLab responded %d: %s", resp.StatusCode, string(body)[:min(len(body), 200)])
		} else {
			t.Logf("  Not ready yet: %v", err)
		}
		time.Sleep(10 * time.Second)
	}
	t.Fatal("GitLab did not become ready in time")
}

func createRootToken(t *testing.T, podman string) string {
	t.Helper()

	// Use the rails console to create a personal access token for root
	railsScript := `token = User.find_by_username('root').personal_access_tokens.create!(name: 'grunter-test', scopes: ['api'], expires_at: 1.day.from_now); token.set_token('glpat-gruntertest1234567'); token.save!; puts token.token`

	cmd := exec.Command(podman, "exec", containerName,
		"gitlab-rails", "runner", railsScript)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Token might already exist from a previous run
		if strings.Contains(stderr.String(), "already been taken") || strings.Contains(stdout.String(), "already been taken") {
			return "glpat-gruntertest1234567"
		}
		t.Fatalf("creating token: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	token := strings.TrimSpace(stdout.String())
	if token == "" {
		t.Fatal("empty token returned from rails console")
	}
	return token
}

func createTestProject(t *testing.T, env *gitlabEnv) int {
	t.Helper()

	payload := map[string]interface{}{
		"name":                 "grunter-integration-test",
		"initialize_with_readme": true,
	}
	data, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", env.URL+"/api/v4/projects", bytes.NewReader(data))
	req.Header.Set("PRIVATE-TOKEN", env.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
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
		// Project might already exist, try to find it
		req2, _ := http.NewRequest("GET", env.URL+"/api/v4/projects?search=grunter-integration-test", nil)
		req2.Header.Set("PRIVATE-TOKEN", env.Token)
		resp2, err := http.DefaultClient.Do(req2)
		if err != nil {
			t.Fatalf("searching project: %v", err)
		}
		defer resp2.Body.Close()
		body2, _ := io.ReadAll(resp2.Body)

		var projects []struct {
			ID int `json:"id"`
		}
		json.Unmarshal(body2, &projects)
		if len(projects) > 0 {
			return projects[0].ID
		}
		t.Fatalf("could not create or find project, response: %s", body)
	}
	return result.ID
}

func createTestMR(t *testing.T, env *gitlabEnv) int {
	t.Helper()

	projectID := strconv.Itoa(env.ProjectID)

	// Create a branch via API
	branchPayload := map[string]string{
		"branch": "test-branch",
		"ref":    "main",
	}
	branchData, _ := json.Marshal(branchPayload)
	req, _ := http.NewRequest("POST",
		fmt.Sprintf("%s/api/v4/projects/%s/repository/branches", env.URL, projectID),
		bytes.NewReader(branchData))
	req.Header.Set("PRIVATE-TOKEN", env.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("creating branch: %v", err)
	}
	resp.Body.Close()

	// Create a file on the branch to have a diff
	filePayload := map[string]string{
		"branch":         "test-branch",
		"content":        "# test change\n",
		"commit_message": "test commit",
	}
	fileData, _ := json.Marshal(filePayload)
	req, _ = http.NewRequest("POST",
		fmt.Sprintf("%s/api/v4/projects/%s/repository/files/%s", env.URL, projectID, "test.txt"),
		bytes.NewReader(fileData))
	req.Header.Set("PRIVATE-TOKEN", env.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("creating file: %v", err)
	}
	resp.Body.Close()

	// Create MR
	mrPayload := map[string]string{
		"source_branch": "test-branch",
		"target_branch": "main",
		"title":         "Grunter integration test MR",
	}
	mrData, _ := json.Marshal(mrPayload)
	req, _ = http.NewRequest("POST",
		fmt.Sprintf("%s/api/v4/projects/%s/merge_requests", env.URL, projectID),
		bytes.NewReader(mrData))
	req.Header.Set("PRIVATE-TOKEN", env.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("creating MR: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var mrResult struct {
		IID int `json:"iid"`
	}
	if err := json.Unmarshal(body, &mrResult); err != nil {
		t.Fatalf("parsing MR response: %v\nbody: %s", err, body)
	}

	if mrResult.IID == 0 {
		// MR might already exist
		req2, _ := http.NewRequest("GET",
			fmt.Sprintf("%s/api/v4/projects/%s/merge_requests?state=opened", env.URL, projectID), nil)
		req2.Header.Set("PRIVATE-TOKEN", env.Token)
		resp2, err := http.DefaultClient.Do(req2)
		if err != nil {
			t.Fatal(err)
		}
		defer resp2.Body.Close()
		body2, _ := io.ReadAll(resp2.Body)
		var mrs []struct {
			IID int `json:"iid"`
		}
		json.Unmarshal(body2, &mrs)
		if len(mrs) > 0 {
			return mrs[0].IID
		}
		t.Fatalf("could not create or find MR, response: %s", body)
	}
	return mrResult.IID
}

func TestClientIntegration_PostComment(t *testing.T) {
	env := setupGitLab(t)

	client := NewClient(env.URL, env.Token)
	projectID := strconv.Itoa(env.ProjectID)
	mrIID := strconv.Itoa(env.MRIID)

	body := "## Test Plan\n\nThis is a test comment from grunter integration tests.\n\n```\nPlan: 1 to add, 0 to change, 0 to destroy.\n```"

	err := client.PostOrUpdateComment(projectID, mrIID, "envs/dev/vpc", body)
	if err != nil {
		t.Fatalf("PostOrUpdateComment failed: %v", err)
	}

	// Verify the comment exists
	notes, err := client.listNotes(projectID, mrIID)
	if err != nil {
		t.Fatalf("listing notes: %v", err)
	}

	found := false
	for _, n := range notes {
		if strings.Contains(n.Body, "envs/dev/vpc") && strings.Contains(n.Body, "Test Plan") {
			found = true
			break
		}
	}
	if !found {
		t.Error("posted comment not found in MR notes")
	}
}

func TestClientIntegration_UpdateExistingComment(t *testing.T) {
	env := setupGitLab(t)

	client := NewClient(env.URL, env.Token)
	projectID := strconv.Itoa(env.ProjectID)
	mrIID := strconv.Itoa(env.MRIID)

	unitPath := "envs/dev/rds-update-test"

	// Post initial comment
	err := client.PostOrUpdateComment(projectID, mrIID, unitPath, "## Initial plan\nFirst run")
	if err != nil {
		t.Fatalf("initial post failed: %v", err)
	}

	// Post again with updated content - should update, not create new
	err = client.PostOrUpdateComment(projectID, mrIID, unitPath, "## Updated plan\nSecond run with changes")
	if err != nil {
		t.Fatalf("update post failed: %v", err)
	}

	// Count comments with our marker
	notes, err := client.listNotes(projectID, mrIID)
	if err != nil {
		t.Fatal(err)
	}

	marker := markerPrefix + unitPath + markerSuffix
	count := 0
	var matchedBody string
	for _, n := range notes {
		if strings.Contains(n.Body, marker) {
			count++
			matchedBody = n.Body
		}
	}

	if count != 1 {
		t.Errorf("expected exactly 1 comment with marker, got %d", count)
	}
	if !strings.Contains(matchedBody, "Updated plan") {
		t.Error("comment was not updated with new content")
	}
	if strings.Contains(matchedBody, "Initial plan") {
		t.Error("comment still contains old content")
	}
}

func TestClientIntegration_MultipleUnitsGetSeparateComments(t *testing.T) {
	env := setupGitLab(t)

	client := NewClient(env.URL, env.Token)
	projectID := strconv.Itoa(env.ProjectID)
	mrIID := strconv.Itoa(env.MRIID)

	units := []string{
		"envs/dev/multi-vpc",
		"envs/dev/multi-rds",
		"envs/dev/multi-app",
	}

	for _, unit := range units {
		body := fmt.Sprintf("## Plan for %s\nPlan: 1 to add", filepath.Base(unit))
		err := client.PostOrUpdateComment(projectID, mrIID, unit, body)
		if err != nil {
			t.Fatalf("posting comment for %s: %v", unit, err)
		}
	}

	// Verify each unit has its own comment
	notes, err := client.listNotes(projectID, mrIID)
	if err != nil {
		t.Fatal(err)
	}

	for _, unit := range units {
		marker := markerPrefix + unit + markerSuffix
		found := false
		for _, n := range notes {
			if strings.Contains(n.Body, marker) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no comment found for unit %s", unit)
		}
	}
}

func TestClientIntegration_SetCommitStatus(t *testing.T) {
	env := setupGitLab(t)

	client := NewClient(env.URL, env.Token)
	projectID := strconv.Itoa(env.ProjectID)

	// Get a valid commit SHA from the project
	sha := getProjectHeadSHA(t, env)
	t.Logf("Setting commit status on %s", sha)

	err := client.SetCommitStatus(projectID, sha, "success", "grunter/plan", "All plans succeeded", "")
	if err != nil {
		t.Fatalf("SetCommitStatus failed: %v", err)
	}

	// Verify via API
	req, _ := http.NewRequest("GET",
		fmt.Sprintf("%s/api/v4/projects/%s/repository/commits/%s/statuses", env.URL, projectID, sha), nil)
	req.Header.Set("PRIVATE-TOKEN", env.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var statuses []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	json.Unmarshal(body, &statuses)

	found := false
	for _, s := range statuses {
		if s.Name == "grunter/plan" && s.Status == "success" {
			found = true
		}
	}
	if !found {
		t.Errorf("commit status not found, got: %s", body)
	}
}

// listNotes is a helper to list all notes on a MR.
func (c *Client) listNotes(projectID, mrIID string) ([]note, error) {
	endpoint := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%s/notes?per_page=100",
		c.baseURL, projectID, mrIID)
	var notes []note
	if err := c.doJSON(http.MethodGet, endpoint, nil, &notes); err != nil {
		return nil, err
	}
	return notes, nil
}

func getProjectHeadSHA(t *testing.T, env *gitlabEnv) string {
	t.Helper()
	projectID := strconv.Itoa(env.ProjectID)
	req, _ := http.NewRequest("GET",
		fmt.Sprintf("%s/api/v4/projects/%s/repository/commits?per_page=1", env.URL, projectID), nil)
	req.Header.Set("PRIVATE-TOKEN", env.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var commits []struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &commits)
	if len(commits) == 0 {
		t.Fatal("no commits found in project")
	}
	return commits[0].ID
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
