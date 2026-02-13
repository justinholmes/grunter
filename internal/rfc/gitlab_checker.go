package rfc

import (
	"fmt"
	"strconv"

	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/gitlab"
)

// GitLabChecker verifies RFC approval by checking MR labels and linked issue labels.
type GitLabChecker struct {
	Client    *gitlab.Client
	ProjectID string
	MRIID     string
	CommitSHA string
	Config    *config.RFCConfig
}

func (g *GitLabChecker) CheckRFC() (*CheckResult, error) {
	mrIID := g.MRIID

	// In post-merge pipelines, CI_MERGE_REQUEST_IID is unavailable.
	// Fall back to looking up the MR from the commit SHA.
	if mrIID == "" && g.CommitSHA != "" {
		mrs, err := g.Client.GetMRsForCommit(g.ProjectID, g.CommitSHA)
		if err != nil {
			return nil, fmt.Errorf("looking up MR for commit %s: %w", g.CommitSHA, err)
		}
		if len(mrs) == 0 {
			return &CheckResult{
				Approved: false,
				Message:  fmt.Sprintf("no merge request found for commit %s", g.CommitSHA),
			}, nil
		}
		mrIID = strconv.Itoa(mrs[0].IID)
	}

	if mrIID == "" {
		return &CheckResult{
			Approved: false,
			Message:  "no MR IID available (set --mr-iid or CI_MERGE_REQUEST_IID)",
		}, nil
	}

	// Check MR labels for the approved label
	mrLabels, err := g.Client.GetMRLabels(g.ProjectID, mrIID)
	if err != nil {
		return nil, fmt.Errorf("getting MR labels: %w", err)
	}

	if !containsLabel(mrLabels, g.Config.ApprovedLabel) {
		return &CheckResult{
			Approved:      false,
			RFCIdentifier: fmt.Sprintf("MR !%s", mrIID),
			Message:       fmt.Sprintf("MR !%s is missing label %q", mrIID, g.Config.ApprovedLabel),
		}, nil
	}

	// Check for emergency label on MR
	emergency := g.Config.EmergencyLabel != "" && containsLabel(mrLabels, g.Config.EmergencyLabel)

	// If issue_approved_label is configured, verify linked issues
	if g.Config.IssueApprovedLabel != "" {
		issues, err := g.Client.GetMRLinkedIssues(g.ProjectID, mrIID)
		if err != nil {
			return nil, fmt.Errorf("getting linked issues: %w", err)
		}

		if len(issues) == 0 {
			return &CheckResult{
				Approved:      false,
				RFCIdentifier: fmt.Sprintf("MR !%s", mrIID),
				Message:       fmt.Sprintf("MR !%s has no linked issues (expected issue with label %q)", mrIID, g.Config.IssueApprovedLabel),
			}, nil
		}

		issueApproved := false
		for _, issue := range issues {
			labels, err := g.Client.GetIssueLabels(g.ProjectID, issue.IID)
			if err != nil {
				return nil, fmt.Errorf("getting labels for issue #%d: %w", issue.IID, err)
			}
			if containsLabel(labels, g.Config.IssueApprovedLabel) {
				issueApproved = true
				break
			}
		}

		if !issueApproved {
			return &CheckResult{
				Approved:      false,
				RFCIdentifier: fmt.Sprintf("MR !%s", mrIID),
				Message:       fmt.Sprintf("no linked issue has label %q", g.Config.IssueApprovedLabel),
			}, nil
		}
	}

	return &CheckResult{
		Approved:      true,
		Emergency:     emergency,
		RFCIdentifier: fmt.Sprintf("MR !%s", mrIID),
		Message:       fmt.Sprintf("RFC approved via MR !%s", mrIID),
	}, nil
}

func containsLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}
