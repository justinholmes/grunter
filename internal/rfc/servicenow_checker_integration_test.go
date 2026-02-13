//go:build integration

package rfc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/justinholmes/grunter/internal/config"
)

type snowTestEnv struct {
	instanceURL string
	username    string
	password    string
	client      *http.Client
}

func setupServiceNow(t *testing.T) *snowTestEnv {
	t.Helper()

	instanceURL := os.Getenv("GRUNTER_TEST_SNOW_INSTANCE_URL")
	username := os.Getenv("GRUNTER_TEST_SNOW_USERNAME")
	password := os.Getenv("GRUNTER_TEST_SNOW_PASSWORD")

	if instanceURL == "" || username == "" || password == "" {
		t.Skip("skipping: set GRUNTER_TEST_SNOW_INSTANCE_URL, GRUNTER_TEST_SNOW_USERNAME, GRUNTER_TEST_SNOW_PASSWORD")
	}

	return &snowTestEnv{
		instanceURL: instanceURL,
		username:    username,
		password:    password,
		client:      &http.Client{},
	}
}

type snowCreateResult struct {
	Result struct {
		SysID  string `json:"sys_id"`
		Number string `json:"number"`
	} `json:"result"`
}

// createTestChangeRequest creates a change request via the ServiceNow Table API
// and returns the change number and sys_id.
func (e *snowTestEnv) createTestChangeRequest(t *testing.T, fields map[string]string) (string, string) {
	t.Helper()

	payload := map[string]string{
		"short_description": fmt.Sprintf("grunter integration test %s", t.Name()),
	}
	for k, v := range fields {
		payload[k] = v
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshaling create payload: %v", err)
	}

	url := fmt.Sprintf("%s/api/now/table/change_request", e.instanceURL)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.SetBasicAuth(e.username, e.password)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("creating change request: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create change request returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result snowCreateResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decoding create response: %v", err)
	}

	number := result.Result.Number
	sysID := result.Result.SysID
	t.Logf("created change request %s (sys_id=%s)", number, sysID)

	t.Cleanup(func() {
		e.deleteChangeRequest(t, sysID)
	})

	return number, sysID
}

// updateChangeRequest patches fields on an existing change request.
func (e *snowTestEnv) updateChangeRequest(t *testing.T, sysID string, fields map[string]string) {
	t.Helper()

	body, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshaling update payload: %v", err)
	}

	url := fmt.Sprintf("%s/api/now/table/change_request/%s", e.instanceURL, sysID)
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("creating update request: %v", err)
	}
	req.SetBasicAuth(e.username, e.password)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("updating change request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("update change request returned %d: %s", resp.StatusCode, string(respBody))
	}

	t.Logf("updated change request %s: %v", sysID, fields)
}

// deleteChangeRequest removes a change request for cleanup.
func (e *snowTestEnv) deleteChangeRequest(t *testing.T, sysID string) {
	t.Helper()

	url := fmt.Sprintf("%s/api/now/table/change_request/%s", e.instanceURL, sysID)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Logf("warning: creating delete request: %v", err)
		return
	}
	req.SetBasicAuth(e.username, e.password)
	req.Header.Set("Accept", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		t.Logf("warning: deleting change request %s: %v", sysID, err)
		return
	}
	resp.Body.Close()

	t.Logf("deleted change request %s (status %d)", sysID, resp.StatusCode)
}

func TestServiceNowCheckerIntegration_NotApproved(t *testing.T) {
	env := setupServiceNow(t)

	number, _ := env.createTestChangeRequest(t, map[string]string{
		"type": "normal",
	})

	checker := &ServiceNowChecker{
		InstanceURL:  env.instanceURL,
		Username:     env.username,
		Password:     env.password,
		ChangeNumber: number,
		Config: &config.RFCConfig{
			Source:          config.RFCSourceServiceNow,
			InstanceURL:     env.instanceURL,
			ChangeNumberVar: "SNOW_CHG_NUMBER",
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Approved {
		t.Errorf("expected not approved, got: %s", result.Message)
	}
	if result.Message == "" {
		t.Error("expected message to contain approval status info")
	}
	t.Logf("result: approved=%v message=%q", result.Approved, result.Message)
}

func TestServiceNowCheckerIntegration_Approved(t *testing.T) {
	env := setupServiceNow(t)

	number, sysID := env.createTestChangeRequest(t, map[string]string{
		"type": "normal",
	})
	env.updateChangeRequest(t, sysID, map[string]string{
		"approval": "approved",
	})

	checker := &ServiceNowChecker{
		InstanceURL:  env.instanceURL,
		Username:     env.username,
		Password:     env.password,
		ChangeNumber: number,
		Config: &config.RFCConfig{
			Source:          config.RFCSourceServiceNow,
			InstanceURL:     env.instanceURL,
			EmergencyLabel:  "emergency",
			ChangeNumberVar: "SNOW_CHG_NUMBER",
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
		t.Error("expected emergency=false for normal change")
	}
	t.Logf("result: approved=%v emergency=%v message=%q", result.Approved, result.Emergency, result.Message)
}

func TestServiceNowCheckerIntegration_Emergency(t *testing.T) {
	env := setupServiceNow(t)

	number, sysID := env.createTestChangeRequest(t, map[string]string{
		"type": "emergency",
	})
	env.updateChangeRequest(t, sysID, map[string]string{
		"approval": "approved",
	})

	checker := &ServiceNowChecker{
		InstanceURL:  env.instanceURL,
		Username:     env.username,
		Password:     env.password,
		ChangeNumber: number,
		Config: &config.RFCConfig{
			Source:          config.RFCSourceServiceNow,
			InstanceURL:     env.instanceURL,
			EmergencyLabel:  "emergency",
			ChangeNumberVar: "SNOW_CHG_NUMBER",
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
	t.Logf("result: approved=%v emergency=%v message=%q", result.Approved, result.Emergency, result.Message)
}

func TestServiceNowCheckerIntegration_NotFound(t *testing.T) {
	env := setupServiceNow(t)

	checker := &ServiceNowChecker{
		InstanceURL:  env.instanceURL,
		Username:     env.username,
		Password:     env.password,
		ChangeNumber: "CHG9999999",
		Config: &config.RFCConfig{
			Source:          config.RFCSourceServiceNow,
			InstanceURL:     env.instanceURL,
			ChangeNumberVar: "SNOW_CHG_NUMBER",
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Approved {
		t.Error("expected not approved for non-existent change")
	}
	if result.Message == "" {
		t.Error("expected message to mention not found")
	}
	t.Logf("result: approved=%v message=%q", result.Approved, result.Message)
}

func TestServiceNowCheckerIntegration_NoChangeNumber(t *testing.T) {
	env := setupServiceNow(t)

	checker := &ServiceNowChecker{
		InstanceURL:  env.instanceURL,
		Username:     env.username,
		Password:     env.password,
		ChangeNumber: "", // intentionally empty
		Config: &config.RFCConfig{
			Source:          config.RFCSourceServiceNow,
			InstanceURL:     env.instanceURL,
			ChangeNumberVar: "SNOW_CHG_NUMBER",
		},
	}

	result, err := checker.CheckRFC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Approved {
		t.Error("expected not approved when no change number provided")
	}
	if result.Message == "" {
		t.Error("expected message about missing change number")
	}
	t.Logf("result: approved=%v message=%q", result.Approved, result.Message)
}
