//go:build integration

package gitlab

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	testhelpers "github.com/justinholmes/grunter/test"
)

func TestClientIntegration_PostComment(t *testing.T) {
	env := testhelpers.SetupGitLab(t)

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
	env := testhelpers.SetupGitLab(t)

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
	env := testhelpers.SetupGitLab(t)

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
	env := testhelpers.SetupGitLab(t)

	client := NewClient(env.URL, env.Token)
	projectID := strconv.Itoa(env.ProjectID)

	sha := env.GetHeadSHA(t, env.ProjectID)
	t.Logf("Setting commit status on %s", sha)

	err := client.SetCommitStatus(projectID, sha, "success", "grunter/plan", "All plans succeeded", "")
	if err != nil {
		t.Fatalf("SetCommitStatus failed: %v", err)
	}

	// Verify via API
	var statuses []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	env.APIRequest(t, "GET",
		fmt.Sprintf("/api/v4/projects/%d/repository/commits/%s/statuses", env.ProjectID, sha), nil, &statuses)

	found := false
	for _, s := range statuses {
		if s.Name == "grunter/plan" && s.Status == "success" {
			found = true
		}
	}
	if !found {
		t.Errorf("commit status not found, got: %+v", statuses)
	}
}

func TestClientIntegration_GetMRLabels(t *testing.T) {
	env := testhelpers.SetupGitLab(t)

	client := NewClient(env.URL, env.Token)
	projectID := strconv.Itoa(env.ProjectID)
	mrIID := strconv.Itoa(env.MRIID)

	// Create labels and set them on the MR
	env.CreateLabel(t, env.ProjectID, "rfc-approved", "#0033cc")
	env.CreateLabel(t, env.ProjectID, "priority::high", "#d9534f")
	env.SetMRLabels(t, env.ProjectID, env.MRIID, []string{"rfc-approved", "priority::high"})

	labels, err := client.GetMRLabels(projectID, mrIID)
	if err != nil {
		t.Fatalf("GetMRLabels failed: %v", err)
	}

	labelSet := make(map[string]bool)
	for _, l := range labels {
		labelSet[l] = true
	}

	if !labelSet["rfc-approved"] {
		t.Errorf("expected label 'rfc-approved' in %v", labels)
	}
	if !labelSet["priority::high"] {
		t.Errorf("expected label 'priority::high' in %v", labels)
	}
}

func TestClientIntegration_GetIssueLabels(t *testing.T) {
	env := testhelpers.SetupGitLab(t)

	client := NewClient(env.URL, env.Token)
	projectID := strconv.Itoa(env.ProjectID)

	env.CreateLabel(t, env.ProjectID, "status::approved", "#5cb85c")
	issueIID := env.CreateIssue(t, env.ProjectID, "RFC: test issue for label check", []string{"status::approved"})

	labels, err := client.GetIssueLabels(projectID, issueIID)
	if err != nil {
		t.Fatalf("GetIssueLabels failed: %v", err)
	}

	found := false
	for _, l := range labels {
		if l == "status::approved" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected label 'status::approved' in %v", labels)
	}
}

func TestClientIntegration_GetMRLinkedIssues(t *testing.T) {
	env := testhelpers.SetupGitLab(t)

	client := NewClient(env.URL, env.Token)
	projectID := strconv.Itoa(env.ProjectID)
	mrIID := strconv.Itoa(env.MRIID)

	// Create an issue, then link it via MR description
	issueIID := env.CreateIssue(t, env.ProjectID, "RFC: linked issue test", nil)
	env.UpdateMRDescription(t, env.ProjectID, env.MRIID, fmt.Sprintf("Closes #%d", issueIID))

	issues, err := client.GetMRLinkedIssues(projectID, mrIID)
	if err != nil {
		t.Fatalf("GetMRLinkedIssues failed: %v", err)
	}

	found := false
	for _, issue := range issues {
		if issue.IID == issueIID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected issue #%d in linked issues, got %+v", issueIID, issues)
	}
}

func TestClientIntegration_GetMRsForCommit(t *testing.T) {
	env := testhelpers.SetupGitLab(t)

	client := NewClient(env.URL, env.Token)
	projectID := strconv.Itoa(env.ProjectID)

	// Get the head SHA of the test-branch (which is the source of the test MR)
	sha := env.GetBranchHeadSHA(t, env.ProjectID, "test-branch")
	t.Logf("Looking up MRs for commit %s", sha)

	// GitLab may take a moment to index the commit-MR relationship,
	// so retry a few times with a short delay.
	var mrs []MergeRequest
	found := false
	for attempt := 0; attempt < 10; attempt++ {
		var err error
		mrs, err = client.GetMRsForCommit(projectID, sha)
		if err != nil {
			t.Fatalf("GetMRsForCommit failed: %v", err)
		}
		for _, mr := range mrs {
			if mr.IID == env.MRIID {
				found = true
				break
			}
		}
		if found {
			break
		}
		t.Logf("  attempt %d: MR not yet indexed for commit, retrying...", attempt+1)
		time.Sleep(3 * time.Second)
	}
	if !found {
		t.Errorf("expected MR IID %d in results, got %+v", env.MRIID, mrs)
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
