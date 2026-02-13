//go:build integration

package rfc

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/gitlab"
	testhelpers "github.com/justinholmes/grunter/test"
)

// setupRFCProject creates an isolated project with common labels, a branch,
// a file on that branch, and an open MR. Returns the env, project ID, and MR IID.
func setupRFCProject(t *testing.T, name string) (*testhelpers.GitLabTestEnv, int, int) {
	t.Helper()

	env := testhelpers.SetupGitLab(t)

	projectID := env.CreateProject(t, name)
	t.Logf("RFC test project %q: ID=%d", name, projectID)

	// Create standard labels
	env.CreateLabel(t, projectID, "rfc-approved", "#0033cc")
	env.CreateLabel(t, projectID, "status::approved", "#5cb85c")
	env.CreateLabel(t, projectID, "emergency", "#d9534f")

	// Create branch + file + MR (idempotent for re-runs)
	env.CreateBranch(t, projectID, "feature-branch", "main")
	env.CreateOrUpdateFile(t, projectID, "feature-branch", "change.txt", "infra change\n")
	mrIID := env.CreateMR(t, projectID, "feature-branch", "main", "RFC test MR: "+name)
	t.Logf("RFC test MR IID: %d", mrIID)

	return env, projectID, mrIID
}

func TestGitLabCheckerIntegration_FullApproval(t *testing.T) {
	env, projectID, mrIID := setupRFCProject(t, "rfc-full-approval")

	// Add rfc-approved label to MR
	env.SetMRLabels(t, projectID, mrIID, []string{"rfc-approved"})

	// Create issue with status::approved and link it to MR
	issueIID := env.CreateIssue(t, projectID, "RFC: full approval test", []string{"status::approved"})
	env.UpdateMRDescription(t, projectID, mrIID, fmt.Sprintf("Closes #%d", issueIID))

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(env.URL, env.Token),
		ProjectID: strconv.Itoa(projectID),
		MRIID:     strconv.Itoa(mrIID),
		Config: &config.RFCConfig{
			Source:             config.RFCSourceGitLab,
			ApprovedLabel:      "rfc-approved",
			IssueApprovedLabel: "status::approved",
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Errorf("expected approved, got: %s", result.Message)
	}
	if result.Emergency {
		t.Error("expected emergency=false")
	}
}

func TestGitLabCheckerIntegration_MissingMRLabel(t *testing.T) {
	env, projectID, mrIID := setupRFCProject(t, "rfc-missing-mr-label")

	// Do not add rfc-approved label — leave MR unlabeled
	env.SetMRLabels(t, projectID, mrIID, []string{})

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(env.URL, env.Token),
		ProjectID: strconv.Itoa(projectID),
		MRIID:     strconv.Itoa(mrIID),
		Config: &config.RFCConfig{
			Source:        config.RFCSourceGitLab,
			ApprovedLabel: "rfc-approved",
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Approved {
		t.Error("expected not approved when MR label is missing")
	}
}

func TestGitLabCheckerIntegration_MissingIssueLabel(t *testing.T) {
	env, projectID, mrIID := setupRFCProject(t, "rfc-missing-issue-label")

	env.SetMRLabels(t, projectID, mrIID, []string{"rfc-approved"})

	// Create issue WITHOUT status::approved
	issueIID := env.CreateIssue(t, projectID, "RFC: missing issue label test", []string{})
	env.UpdateMRDescription(t, projectID, mrIID, fmt.Sprintf("Closes #%d", issueIID))

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(env.URL, env.Token),
		ProjectID: strconv.Itoa(projectID),
		MRIID:     strconv.Itoa(mrIID),
		Config: &config.RFCConfig{
			Source:             config.RFCSourceGitLab,
			ApprovedLabel:      "rfc-approved",
			IssueApprovedLabel: "status::approved",
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Approved {
		t.Error("expected not approved when linked issue lacks approved label")
	}
}

func TestGitLabCheckerIntegration_NoLinkedIssues(t *testing.T) {
	env, projectID, mrIID := setupRFCProject(t, "rfc-no-linked-issues")

	env.SetMRLabels(t, projectID, mrIID, []string{"rfc-approved"})
	// Do not link any issues

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(env.URL, env.Token),
		ProjectID: strconv.Itoa(projectID),
		MRIID:     strconv.Itoa(mrIID),
		Config: &config.RFCConfig{
			Source:             config.RFCSourceGitLab,
			ApprovedLabel:      "rfc-approved",
			IssueApprovedLabel: "status::approved",
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Approved {
		t.Error("expected not approved when no issues are linked")
	}
}

func TestGitLabCheckerIntegration_Emergency(t *testing.T) {
	env, projectID, mrIID := setupRFCProject(t, "rfc-emergency")

	env.SetMRLabels(t, projectID, mrIID, []string{"rfc-approved", "emergency"})

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(env.URL, env.Token),
		ProjectID: strconv.Itoa(projectID),
		MRIID:     strconv.Itoa(mrIID),
		Config: &config.RFCConfig{
			Source:         config.RFCSourceGitLab,
			ApprovedLabel:  "rfc-approved",
			EmergencyLabel: "emergency",
			// No IssueApprovedLabel — skip issue check
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Errorf("expected approved, got: %s", result.Message)
	}
	if !result.Emergency {
		t.Error("expected emergency flag to be set")
	}
}

func TestGitLabCheckerIntegration_MRLabelOnly(t *testing.T) {
	env, projectID, mrIID := setupRFCProject(t, "rfc-mr-label-only")

	env.SetMRLabels(t, projectID, mrIID, []string{"rfc-approved"})

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(env.URL, env.Token),
		ProjectID: strconv.Itoa(projectID),
		MRIID:     strconv.Itoa(mrIID),
		Config: &config.RFCConfig{
			Source:        config.RFCSourceGitLab,
			ApprovedLabel: "rfc-approved",
			// No IssueApprovedLabel — only MR label required
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Errorf("expected approved with MR label only, got: %s", result.Message)
	}
}

func TestGitLabCheckerIntegration_PostMergeCommitSHA(t *testing.T) {
	env := testhelpers.SetupGitLab(t)

	projectID := env.CreateProject(t, "rfc-post-merge")
	env.CreateLabel(t, projectID, "rfc-approved", "#0033cc")

	// Use a unique branch name so re-runs don't collide with merged MRs
	branch := fmt.Sprintf("merge-test-%d", time.Now().UnixNano())
	filename := fmt.Sprintf("change-%d.txt", time.Now().UnixNano())
	env.CreateBranch(t, projectID, branch, "main")
	env.CreateFileOnBranch(t, projectID, branch, filename, "infra change "+branch+"\n")
	mrIID := env.CreateMR(t, projectID, branch, "main", "RFC post-merge test: "+branch)

	env.SetMRLabels(t, projectID, mrIID, []string{"rfc-approved"})

	// Merge the MR to get a merge commit SHA
	mergeCommitSHA := env.MergeMR(t, projectID, mrIID)
	t.Logf("Merge commit SHA: %s", mergeCommitSHA)

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(env.URL, env.Token),
		ProjectID: strconv.Itoa(projectID),
		MRIID:     "", // No MR IID — simulate post-merge pipeline
		CommitSHA: mergeCommitSHA,
		Config: &config.RFCConfig{
			Source:        config.RFCSourceGitLab,
			ApprovedLabel: "rfc-approved",
			// No IssueApprovedLabel — only MR label required
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Errorf("expected approved via post-merge commit SHA lookup, got: %s", result.Message)
	}
}
