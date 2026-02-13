package rfc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCustomWindowChecker_Allowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"is_open": true,
		})
	}))
	defer srv.Close()

	checker := &CustomWindowChecker{
		URL:          srv.URL,
		AllowedField: "is_open",
	}

	result, err := checker.CheckWindow()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Errorf("expected allowed, got: %s", result.Message)
	}
}

func TestCustomWindowChecker_NotAllowed_WithMessageAndNextWindow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"is_open":      false,
			"reason":       "maintenance window",
			"next_open_at": "2025-06-15T09:00:00Z",
		})
	}))
	defer srv.Close()

	checker := &CustomWindowChecker{
		URL:             srv.URL,
		AllowedField:    "is_open",
		MessageField:    "reason",
		NextWindowField: "next_open_at",
	}

	result, err := checker.CheckWindow()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected not allowed")
	}
	if result.Message != "maintenance window" {
		t.Errorf("expected message 'maintenance window', got %q", result.Message)
	}
	if result.NextWindowStart == nil {
		t.Fatal("expected NextWindowStart to be set")
	}
	if result.NextWindowStart.Format("2006-01-02T15:04:05Z") != "2025-06-15T09:00:00Z" {
		t.Errorf("unexpected next window start: %s", result.NextWindowStart)
	}
}

func TestCustomWindowChecker_NotAllowed_NoOptionalFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"is_open": false,
		})
	}))
	defer srv.Close()

	checker := &CustomWindowChecker{
		URL:          srv.URL,
		AllowedField: "is_open",
	}

	result, err := checker.CheckWindow()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected not allowed")
	}
	if result.Message != "deployment window is closed" {
		t.Errorf("expected default message, got %q", result.Message)
	}
	if result.NextWindowStart != nil {
		t.Error("expected no NextWindowStart")
	}
}

func TestCustomWindowChecker_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer srv.Close()

	checker := &CustomWindowChecker{
		URL:          srv.URL,
		AllowedField: "is_open",
	}

	_, err := checker.CheckWindow()
	if err == nil {
		t.Fatal("expected error for API failure")
	}
}

func TestCustomWindowChecker_StringBoolValues(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		allowed bool
	}{
		{"string true", "true", true},
		{"string false", "false", false},
		{"bool true", true, true},
		{"bool false", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"is_open": tt.value,
				})
			}))
			defer srv.Close()

			checker := &CustomWindowChecker{
				URL:          srv.URL,
				AllowedField: "is_open",
			}

			result, err := checker.CheckWindow()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Allowed != tt.allowed {
				t.Errorf("expected allowed=%v, got %v", tt.allowed, result.Allowed)
			}
		})
	}
}

func TestCustomWindowChecker_ResponsePath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"window": map[string]interface{}{
					"is_open": true,
				},
			},
		})
	}))
	defer srv.Close()

	checker := &CustomWindowChecker{
		URL:          srv.URL,
		ResponsePath: "data.window",
		AllowedField: "is_open",
	}

	result, err := checker.CheckWindow()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Errorf("expected allowed with nested response path, got: %s", result.Message)
	}
}

func TestCustomWindowChecker_BearerAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer deploy-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"is_open": true,
		})
	}))
	defer srv.Close()

	checker := &CustomWindowChecker{
		URL:          srv.URL,
		AuthType:     "bearer",
		AuthToken:    "deploy-token",
		AllowedField: "is_open",
	}

	result, err := checker.CheckWindow()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Errorf("expected allowed with bearer auth, got: %s", result.Message)
	}
}
