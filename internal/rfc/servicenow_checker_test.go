package rfc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/justinholmes/grunter/internal/config"
)

func TestServiceNowChecker_Approved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify basic auth
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		json.NewEncoder(w).Encode(snowResult{
			Result: []snowChangeRequest{
				{Number: "CHG001", Approval: "approved", Type: "normal"},
			},
		})
	}))
	defer srv.Close()

	checker := &ServiceNowChecker{
		InstanceURL:  srv.URL,
		Username:     "admin",
		Password:     "secret",
		ChangeNumber: "CHG001",
		Config: &config.RFCConfig{
			Source:      config.RFCSourceServiceNow,
			InstanceURL: srv.URL,
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
		t.Error("expected non-emergency")
	}
}

func TestServiceNowChecker_NotApproved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(snowResult{
			Result: []snowChangeRequest{
				{Number: "CHG002", Approval: "requested", Type: "normal"},
			},
		})
	}))
	defer srv.Close()

	checker := &ServiceNowChecker{
		InstanceURL:  srv.URL,
		Username:     "admin",
		Password:     "secret",
		ChangeNumber: "CHG002",
		Config: &config.RFCConfig{
			Source:      config.RFCSourceServiceNow,
			InstanceURL: srv.URL,
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Approved {
		t.Error("expected not approved")
	}
}

func TestServiceNowChecker_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(snowResult{Result: []snowChangeRequest{}})
	}))
	defer srv.Close()

	checker := &ServiceNowChecker{
		InstanceURL:  srv.URL,
		Username:     "admin",
		Password:     "secret",
		ChangeNumber: "CHG999",
		Config: &config.RFCConfig{
			Source:      config.RFCSourceServiceNow,
			InstanceURL: srv.URL,
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Approved {
		t.Error("expected not approved when change not found")
	}
}

func TestServiceNowChecker_Emergency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(snowResult{
			Result: []snowChangeRequest{
				{Number: "CHG003", Approval: "approved", Type: "emergency"},
			},
		})
	}))
	defer srv.Close()

	checker := &ServiceNowChecker{
		InstanceURL:  srv.URL,
		Username:     "admin",
		Password:     "secret",
		ChangeNumber: "CHG003",
		Config: &config.RFCConfig{
			Source:         config.RFCSourceServiceNow,
			InstanceURL:    srv.URL,
			EmergencyLabel: "emergency",
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

func TestServiceNowChecker_NoChangeNumber(t *testing.T) {
	checker := &ServiceNowChecker{
		InstanceURL: "https://example.service-now.com",
		Username:    "admin",
		Password:    "secret",
		Config: &config.RFCConfig{
			Source:          config.RFCSourceServiceNow,
			ChangeNumberVar: "SNOW_CHG_NUMBER",
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Approved {
		t.Error("expected not approved when no change number")
	}
}

func TestServiceNowChecker_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer srv.Close()

	checker := &ServiceNowChecker{
		InstanceURL:  srv.URL,
		Username:     "admin",
		Password:     "secret",
		ChangeNumber: "CHG001",
		Config: &config.RFCConfig{
			Source:      config.RFCSourceServiceNow,
			InstanceURL: srv.URL,
		},
	}

	_, err := checker.CheckRFC()
	if err == nil {
		t.Fatal("expected error for API failure")
	}
}
