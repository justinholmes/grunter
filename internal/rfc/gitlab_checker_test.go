package rfc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/justinholmes/grunter/internal/config"
	"github.com/justinholmes/grunter/internal/gitlab"
)

func TestGitLabChecker_Approved(t *testing.T) {
	mux := http.NewServeMux()

	// MR has approved label
	mux.HandleFunc("/api/v4/projects/1/merge_requests/10", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"iid":    10,
			"labels": []string{"rfc-approved"},
		})
	})

	// Linked issues
	mux.HandleFunc("/api/v4/projects/1/merge_requests/10/closes_issues", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"iid": 5, "title": "RFC: deploy change", "labels": []string{}},
		})
	})

	// Issue labels
	mux.HandleFunc("/api/v4/projects/1/issues/5", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"iid":    5,
			"labels": []string{"status::approved"},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(srv.URL, "test-token"),
		ProjectID: "1",
		MRIID:     "10",
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
}

func TestGitLabChecker_MissingMRLabel(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v4/projects/1/merge_requests/10", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"iid":    10,
			"labels": []string{"other-label"},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(srv.URL, "test-token"),
		ProjectID: "1",
		MRIID:     "10",
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

func TestGitLabChecker_NoLinkedIssues(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v4/projects/1/merge_requests/10", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"iid":    10,
			"labels": []string{"rfc-approved"},
		})
	})

	mux.HandleFunc("/api/v4/projects/1/merge_requests/10/closes_issues", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(srv.URL, "test-token"),
		ProjectID: "1",
		MRIID:     "10",
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
		t.Error("expected not approved when no linked issues")
	}
}

func TestGitLabChecker_IssueNotApproved(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v4/projects/1/merge_requests/10", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"iid":    10,
			"labels": []string{"rfc-approved"},
		})
	})

	mux.HandleFunc("/api/v4/projects/1/merge_requests/10/closes_issues", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"iid": 5, "title": "RFC", "labels": []string{}},
		})
	})

	mux.HandleFunc("/api/v4/projects/1/issues/5", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"iid":    5,
			"labels": []string{"status::pending"},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(srv.URL, "test-token"),
		ProjectID: "1",
		MRIID:     "10",
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
		t.Error("expected not approved when issue label doesn't match")
	}
}

func TestGitLabChecker_Emergency(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v4/projects/1/merge_requests/10", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"iid":    10,
			"labels": []string{"rfc-approved", "emergency"},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(srv.URL, "test-token"),
		ProjectID: "1",
		MRIID:     "10",
		Config: &config.RFCConfig{
			Source:         config.RFCSourceGitLab,
			ApprovedLabel:  "rfc-approved",
			EmergencyLabel: "emergency",
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Error("expected approved")
	}
	if !result.Emergency {
		t.Error("expected emergency flag to be set")
	}
}

func TestGitLabChecker_PostMerge_CommitSHAFallback(t *testing.T) {
	mux := http.NewServeMux()

	// Commit → MR lookup
	mux.HandleFunc("/api/v4/projects/1/repository/commits/abc123/merge_requests", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"iid": 10, "title": "My MR", "labels": []string{}},
		})
	})

	// MR details
	mux.HandleFunc("/api/v4/projects/1/merge_requests/10", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"iid":    10,
			"labels": []string{"rfc-approved"},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(srv.URL, "test-token"),
		ProjectID: "1",
		MRIID:     "", // no MR IID (post-merge)
		CommitSHA: "abc123",
		Config: &config.RFCConfig{
			Source:        config.RFCSourceGitLab,
			ApprovedLabel: "rfc-approved",
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Errorf("expected approved, got: %s", result.Message)
	}
}

func TestGitLabChecker_NoMRIIDOrSHA(t *testing.T) {
	srv := httptest.NewServer(http.NewServeMux())
	defer srv.Close()

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(srv.URL, "test-token"),
		ProjectID: "1",
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
		t.Error("expected not approved when no MR IID or SHA available")
	}
}

func TestGitLabChecker_ApprovedWithoutIssueLabel(t *testing.T) {
	// When issue_approved_label is not configured, only MR label matters
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v4/projects/1/merge_requests/10", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"iid":    10,
			"labels": []string{"rfc-approved"},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	checker := &GitLabChecker{
		Client:    gitlab.NewClient(srv.URL, "test-token"),
		ProjectID: "1",
		MRIID:     "10",
		Config: &config.RFCConfig{
			Source:        config.RFCSourceGitLab,
			ApprovedLabel: "rfc-approved",
			// no IssueApprovedLabel
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Errorf("expected approved with only MR label check, got: %s", result.Message)
	}
}
