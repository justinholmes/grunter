package rfc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCustomChecker_Approved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "approved",
			"ticket_number": "CHG-100",
		})
	}))
	defer srv.Close()

	checker := &CustomChecker{
		URL:             srv.URL + "/changes/CHG-100",
		ApprovedField:   "status",
		ApprovedValue:   "approved",
		IdentifierField: "ticket_number",
		ChangeID:        "CHG-100",
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Errorf("expected approved, got: %s", result.Message)
	}
	if result.RFCIdentifier != "CHG-100" {
		t.Errorf("expected identifier CHG-100, got %s", result.RFCIdentifier)
	}
}

func TestCustomChecker_NotApproved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "pending",
		})
	}))
	defer srv.Close()

	checker := &CustomChecker{
		URL:           srv.URL,
		ApprovedField: "status",
		ApprovedValue: "approved",
		ChangeID:      "CHG-200",
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Approved {
		t.Error("expected not approved")
	}
}

func TestCustomChecker_Emergency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "approved",
			"priority": "P1",
		})
	}))
	defer srv.Close()

	checker := &CustomChecker{
		URL:            srv.URL,
		ApprovedField:  "status",
		ApprovedValue:  "approved",
		EmergencyField: "priority",
		EmergencyValue: "P1",
		ChangeID:       "CHG-300",
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

func TestCustomChecker_ArrayResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]interface{}{
			map[string]interface{}{
				"status": "approved",
				"id":     "FIRST",
			},
			map[string]interface{}{
				"status": "rejected",
				"id":     "SECOND",
			},
		})
	}))
	defer srv.Close()

	checker := &CustomChecker{
		URL:             srv.URL,
		ResponseType:    "array",
		ApprovedField:   "status",
		ApprovedValue:   "approved",
		IdentifierField: "id",
		ChangeID:        "test",
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Errorf("expected approved (first element), got: %s", result.Message)
	}
	if result.RFCIdentifier != "FIRST" {
		t.Errorf("expected identifier FIRST, got %s", result.RFCIdentifier)
	}
}

func TestCustomChecker_ArrayResponse_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]interface{}{})
	}))
	defer srv.Close()

	checker := &CustomChecker{
		URL:           srv.URL,
		ResponseType:  "array",
		ApprovedField: "status",
		ApprovedValue: "approved",
		ChangeID:      "CHG-404",
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Approved {
		t.Error("expected not approved for empty array")
	}
}

func TestCustomChecker_NestedResponsePath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"change": map[string]interface{}{
					"status": "approved",
				},
			},
		})
	}))
	defer srv.Close()

	checker := &CustomChecker{
		URL:           srv.URL,
		ResponsePath:  "data.change",
		ApprovedField: "status",
		ApprovedValue: "approved",
		ChangeID:      "CHG-500",
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Errorf("expected approved with nested path, got: %s", result.Message)
	}
}

func TestCustomChecker_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer srv.Close()

	checker := &CustomChecker{
		URL:           srv.URL,
		ApprovedField: "status",
		ApprovedValue: "approved",
		ChangeID:      "CHG-ERR",
	}

	_, err := checker.CheckRFC()
	if err == nil {
		t.Fatal("expected error for API failure")
	}
}

func TestCustomChecker_NoChangeID(t *testing.T) {
	checker := &CustomChecker{
		URL:           "https://example.com/api/{{change_id}}",
		ApprovedField: "status",
		ApprovedValue: "approved",
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Approved {
		t.Error("expected not approved when no change ID")
	}
}

func TestCustomChecker_ChangeIDSubstitution(t *testing.T) {
	var receivedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "approved",
		})
	}))
	defer srv.Close()

	checker := &CustomChecker{
		URL:           srv.URL + "/api/changes/{{change_id}}",
		ApprovedField: "status",
		ApprovedValue: "approved",
		ChangeID:      "CHG-123",
	}

	_, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedPath != "/api/changes/CHG-123" {
		t.Errorf("expected path /api/changes/CHG-123, got %s", receivedPath)
	}
}

func TestCustomChecker_BearerAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer my-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "approved",
		})
	}))
	defer srv.Close()

	checker := &CustomChecker{
		URL:           srv.URL,
		AuthType:      "bearer",
		AuthToken:     "my-token",
		ApprovedField: "status",
		ApprovedValue: "approved",
		ChangeID:      "CHG-1",
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Errorf("expected approved with bearer auth, got: %s", result.Message)
	}
}

func TestCustomChecker_BasicAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "approved",
		})
	}))
	defer srv.Close()

	checker := &CustomChecker{
		URL:           srv.URL,
		AuthType:      "basic",
		AuthToken:     "secret",
		AuthUsername:   "admin",
		ApprovedField: "status",
		ApprovedValue: "approved",
		ChangeID:      "CHG-1",
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Errorf("expected approved with basic auth, got: %s", result.Message)
	}
}

func TestCustomChecker_HeaderAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "my-api-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "approved",
		})
	}))
	defer srv.Close()

	checker := &CustomChecker{
		URL:            srv.URL,
		AuthType:       "header",
		AuthToken:      "my-api-key",
		AuthHeaderName: "X-API-Key",
		ApprovedField:  "status",
		ApprovedValue:  "approved",
		ChangeID:       "CHG-1",
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Errorf("expected approved with header auth, got: %s", result.Message)
	}
}

func TestCustomChecker_IDTokenAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer oidc-jwt-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "approved",
		})
	}))
	defer srv.Close()

	checker := &CustomChecker{
		URL:           srv.URL,
		AuthType:      "id_token",
		AuthToken:     "oidc-jwt-token",
		ApprovedField: "status",
		ApprovedValue: "approved",
		ChangeID:      "CHG-1",
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Errorf("expected approved with id_token auth, got: %s", result.Message)
	}
}

func TestCustomChecker_CaseInsensitiveMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "Approved",
		})
	}))
	defer srv.Close()

	checker := &CustomChecker{
		URL:           srv.URL,
		ApprovedField: "status",
		ApprovedValue: "approved",
		ChangeID:      "CHG-1",
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Approved {
		t.Errorf("expected case-insensitive match to approve, got: %s", result.Message)
	}
}
