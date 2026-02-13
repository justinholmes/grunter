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
	"strconv"
	"strings"
	"testing"
	"time"
)

const gitlabImage = "docker.io/gitlab/gitlab-ce:latest"
const containerName = "grunter-test-gitlab"
const gitlabPort = "8929"

// GitLabTestEnv holds connection details for a running GitLab instance.
type GitLabTestEnv struct {
	URL       string
	Token     string
	ProjectID int
	MRIID     int
	podman    string
}

// SetupGitLab returns a GitLabTestEnv connected to a real GitLab instance.
// If GRUNTER_TEST_GITLAB_URL is set, it uses that external instance.
// Otherwise, it starts a Podman/Docker container with GitLab CE.
// It creates a test project and MR for use in tests.
func SetupGitLab(t *testing.T) *GitLabTestEnv {
	t.Helper()

	var env *GitLabTestEnv

	if url := os.Getenv("GRUNTER_TEST_GITLAB_URL"); url != "" {
		env = &GitLabTestEnv{
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
		podman := RequirePodman(t)
		env = &GitLabTestEnv{podman: podman}

		out, err := exec.Command(podman, "ps", "--filter", "name="+containerName, "--format", "{{.ID}}").Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			t.Log("GitLab container already running, reusing")
		} else {
			t.Log("Starting GitLab CE container (this may take a few minutes on first run)...")
			startGitLabContainer(t, podman)
		}

		env.URL = fmt.Sprintf("http://localhost:%s", gitlabPort)
		t.Logf("GitLab URL: %s", env.URL)

		waitForGitLab(t, env.URL, 10*time.Minute)

		env.Token = createRootToken(t, podman)
		t.Logf("Got root token: %s...", env.Token[:8])
	}

	if env.ProjectID == 0 {
		env.ProjectID = env.CreateProject(t, "grunter-integration-test")
		t.Logf("Created project ID: %d", env.ProjectID)
	}

	if env.MRIID == 0 {
		env.CreateBranch(t, env.ProjectID, "test-branch", "main")
		env.CreateOrUpdateFile(t, env.ProjectID, "test-branch", "test.txt", "# test change\n")
		env.MRIID = env.CreateMR(t, env.ProjectID, "test-branch", "main", "Grunter integration test MR")
		t.Logf("Created MR IID: %d", env.MRIID)
	}

	return env
}

func startGitLabContainer(t *testing.T, podman string) {
	t.Helper()

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
		t.Logf("GitLab container %s left running for reuse. Run: podman rm -f %s to clean up.", containerName, containerName)
	})
}

func waitForGitLab(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	// Try multiple endpoints — some GitLab versions disable the monitoring
	// endpoints or put them behind an IP whitelist.
	healthURLs := []string{
		baseURL + "/-/readiness?all=1",
		baseURL + "/-/health",
		baseURL + "/-/liveness",
	}
	apiVersionURL := baseURL + "/api/v4/version"

	t.Logf("Waiting for GitLab to be ready at %s ...", baseURL)

	client := &http.Client{Timeout: 5 * time.Second}
	for time.Now().Before(deadline) {
		// Check health endpoints (200 = ready)
		for _, u := range healthURLs {
			resp, err := client.Get(u)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					t.Logf("GitLab is ready! (%s returned 200)", u)
					return
				}
			}
		}

		// Fallback: if the API responds at all (even 401), GitLab is up
		resp, err := client.Get(apiVersionURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 401 || resp.StatusCode == 200 {
				t.Logf("GitLab is ready! (API returned %d)", resp.StatusCode)
				return
			}
			t.Logf("  GitLab API responded %d, waiting...", resp.StatusCode)
		} else {
			t.Logf("  Not ready yet: %v", err)
		}
		time.Sleep(10 * time.Second)
	}
	t.Fatal("GitLab did not become ready in time")
}

func createRootToken(t *testing.T, podman string) string {
	t.Helper()

	railsScript := `token = User.find_by_username('root').personal_access_tokens.create!(name: 'grunter-test', scopes: ['api'], expires_at: 1.day.from_now); token.set_token('glpat-gruntertest1234567'); token.save!; puts token.token`

	cmd := exec.Command(podman, "exec", containerName,
		"gitlab-rails", "runner", railsScript)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		combinedOutput := stderr.String() + stdout.String()
		if strings.Contains(combinedOutput, "already been taken") ||
			strings.Contains(combinedOutput, "UniqueViolation") ||
			strings.Contains(combinedOutput, "duplicate key") {
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

// CreateProject creates a new project in GitLab. If a project with the given
// name already exists, it returns that project's ID.
func (e *GitLabTestEnv) CreateProject(t *testing.T, name string) int {
	t.Helper()

	var result struct {
		ID int `json:"id"`
	}
	err := e.APIRequest(t, "POST", "/api/v4/projects", map[string]interface{}{
		"name":                   name,
		"initialize_with_readme": true,
	}, &result)

	if err == nil && result.ID != 0 {
		return result.ID
	}

	// Project might already exist, try to find it
	var projects []struct {
		ID int `json:"id"`
	}
	e.APIRequest(t, "GET", fmt.Sprintf("/api/v4/projects?search=%s", name), nil, &projects)
	if len(projects) > 0 {
		return projects[0].ID
	}
	t.Fatalf("could not create or find project %q", name)
	return 0
}

// CreateBranch creates a branch in the given project. It does not fail if the
// branch already exists.
func (e *GitLabTestEnv) CreateBranch(t *testing.T, projectID int, branch, ref string) {
	t.Helper()
	// Ignore error — branch may already exist
	e.APIRequest(t, "POST",
		fmt.Sprintf("/api/v4/projects/%d/repository/branches", projectID),
		map[string]string{"branch": branch, "ref": ref}, nil)
}

// CreateFileOnBranch commits a file to the given branch. Fails if the file
// already exists — use CreateOrUpdateFile for idempotent operations.
func (e *GitLabTestEnv) CreateFileOnBranch(t *testing.T, projectID int, branch, path, content string) {
	t.Helper()
	err := e.APIRequest(t, "POST",
		fmt.Sprintf("/api/v4/projects/%d/repository/files/%s", projectID, path),
		map[string]string{
			"branch":         branch,
			"content":        content,
			"commit_message": "test commit: " + path,
		}, nil)
	if err != nil {
		t.Fatalf("creating file %s on branch %s: %v", path, branch, err)
	}
}

// CreateOrUpdateFile commits a file to the given branch. If the file already
// exists, it updates it instead. This is idempotent.
func (e *GitLabTestEnv) CreateOrUpdateFile(t *testing.T, projectID int, branch, path, content string) {
	t.Helper()
	endpoint := fmt.Sprintf("/api/v4/projects/%d/repository/files/%s", projectID, path)
	payload := map[string]string{
		"branch":         branch,
		"content":        content,
		"commit_message": "test commit: " + path,
	}
	err := e.APIRequest(t, "POST", endpoint, payload, nil)
	if err != nil {
		// File may already exist — try updating
		payload["commit_message"] = "update: " + path
		err2 := e.APIRequest(t, "PUT", endpoint, payload, nil)
		if err2 != nil {
			// Both failed — but if the file exists and content hasn't changed,
			// PUT can also 400. Just log and continue.
			t.Logf("file %s on branch %s: create failed (%v), update failed (%v) — may already exist with same content", path, branch, err, err2)
		}
	}
}

// CreateMR creates a merge request. If an MR already exists for the source
// branch, it returns that MR's IID.
func (e *GitLabTestEnv) CreateMR(t *testing.T, projectID int, source, target, title string) int {
	t.Helper()

	var result struct {
		IID int `json:"iid"`
	}
	err := e.APIRequest(t, "POST",
		fmt.Sprintf("/api/v4/projects/%d/merge_requests", projectID),
		map[string]string{
			"source_branch": source,
			"target_branch": target,
			"title":         title,
		}, &result)

	if err == nil && result.IID != 0 {
		return result.IID
	}

	// MR might already exist
	var mrs []struct {
		IID int `json:"iid"`
	}
	e.APIRequest(t, "GET",
		fmt.Sprintf("/api/v4/projects/%d/merge_requests?state=opened&source_branch=%s", projectID, source), nil, &mrs)
	if len(mrs) > 0 {
		return mrs[0].IID
	}
	t.Fatalf("could not create or find MR from %s to %s", source, target)
	return 0
}

// CreateLabel creates a project label. Does not fail if the label already exists.
func (e *GitLabTestEnv) CreateLabel(t *testing.T, projectID int, name, color string) {
	t.Helper()
	// Ignore error — label may already exist
	e.APIRequest(t, "POST",
		fmt.Sprintf("/api/v4/projects/%d/labels", projectID),
		map[string]string{"name": name, "color": color}, nil)
}

// SetMRLabels replaces the labels on a merge request.
func (e *GitLabTestEnv) SetMRLabels(t *testing.T, projectID, mrIID int, labels []string) {
	t.Helper()
	err := e.APIRequest(t, "PUT",
		fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d", projectID, mrIID),
		map[string]interface{}{"labels": strings.Join(labels, ",")}, nil)
	if err != nil {
		t.Fatalf("setting MR labels: %v", err)
	}
}

// UpdateMRDescription updates the description of a merge request.
func (e *GitLabTestEnv) UpdateMRDescription(t *testing.T, projectID, mrIID int, description string) {
	t.Helper()
	err := e.APIRequest(t, "PUT",
		fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d", projectID, mrIID),
		map[string]string{"description": description}, nil)
	if err != nil {
		t.Fatalf("updating MR description: %v", err)
	}
}

// CreateIssue creates an issue in the project with the given labels.
func (e *GitLabTestEnv) CreateIssue(t *testing.T, projectID int, title string, labels []string) int {
	t.Helper()

	var result struct {
		IID int `json:"iid"`
	}
	err := e.APIRequest(t, "POST",
		fmt.Sprintf("/api/v4/projects/%d/issues", projectID),
		map[string]interface{}{
			"title":  title,
			"labels": strings.Join(labels, ","),
		}, &result)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}
	if result.IID == 0 {
		t.Fatal("created issue has IID 0")
	}
	return result.IID
}

// GetHeadSHA returns the SHA of the most recent commit on the default branch.
func (e *GitLabTestEnv) GetHeadSHA(t *testing.T, projectID int) string {
	t.Helper()

	var commits []struct {
		ID string `json:"id"`
	}
	err := e.APIRequest(t, "GET",
		fmt.Sprintf("/api/v4/projects/%d/repository/commits?per_page=1", projectID), nil, &commits)
	if err != nil {
		t.Fatalf("getting head SHA: %v", err)
	}
	if len(commits) == 0 {
		t.Fatal("no commits found in project")
	}
	return commits[0].ID
}

// GetBranchHeadSHA returns the SHA of the most recent commit on a specific branch.
func (e *GitLabTestEnv) GetBranchHeadSHA(t *testing.T, projectID int, branch string) string {
	t.Helper()

	var result struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	err := e.APIRequest(t, "GET",
		fmt.Sprintf("/api/v4/projects/%d/repository/branches/%s", projectID, branch), nil, &result)
	if err != nil {
		t.Fatalf("getting branch %s head SHA: %v", branch, err)
	}
	return result.Commit.ID
}

// MergeMR merges a merge request and returns the merge commit SHA.
// It waits for the MR to become mergeable before attempting the merge.
func (e *GitLabTestEnv) MergeMR(t *testing.T, projectID, mrIID int) string {
	t.Helper()

	mergeEndpoint := fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d/merge", projectID, mrIID)
	mrEndpoint := fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d", projectID, mrIID)

	// Wait for MR to become mergeable, then attempt merge with retries
	for i := 0; i < 30; i++ {
		var mr struct {
			State               string `json:"state"`
			MergeCommitSHA      string `json:"merge_commit_sha"`
			DetailedMergeStatus string `json:"detailed_merge_status"`
		}
		e.APIRequest(t, "GET", mrEndpoint, nil, &mr)

		if mr.State == "merged" && mr.MergeCommitSHA != "" {
			return mr.MergeCommitSHA
		}

		if mr.DetailedMergeStatus == "mergeable" || mr.DetailedMergeStatus == "ci_must_pass" {
			var result struct {
				MergeCommitSHA string `json:"merge_commit_sha"`
			}
			err := e.APIRequest(t, "PUT", mergeEndpoint, nil, &result)
			if err == nil {
				// Merge accepted — poll for completion
				for j := 0; j < 30; j++ {
					e.APIRequest(t, "GET", mrEndpoint, nil, &mr)
					if mr.State == "merged" && mr.MergeCommitSHA != "" {
						return mr.MergeCommitSHA
					}
					time.Sleep(1 * time.Second)
				}
				t.Fatal("MR accepted for merge but did not complete in time")
			}
			t.Logf("  merge attempt %d failed (%v), retrying...", i+1, err)
		} else {
			t.Logf("  MR not yet mergeable (status=%s), waiting...", mr.DetailedMergeStatus)
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("MR did not become mergeable in time")
	return ""
}

// APIRequest performs an HTTP request against the GitLab API. If payload is
// non-nil, it is JSON-encoded and sent as the request body. If result is
// non-nil, the response body is JSON-decoded into it.
func (e *GitLabTestEnv) APIRequest(t *testing.T, method, path string, payload, result interface{}) error {
	t.Helper()

	var bodyReader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshaling payload: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := e.URL + path
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("PRIVATE-TOKEN", e.Token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %s %s returned %d: %s", method, path, resp.StatusCode, body)
	}

	if result != nil {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("decoding response from %s %s: %w\nbody: %s", method, path, err, body)
		}
	}
	return nil
}

