package rfc

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// CustomChecker verifies RFC approval by querying a user-configured HTTP API.
type CustomChecker struct {
	HTTPClient *http.Client
	URL        string // may contain {{change_id}} placeholder
	Method     string // GET, POST, etc. Default: GET

	// Auth
	AuthType       string // "bearer", "basic", or "header"
	AuthToken      string // resolved credential
	AuthUsername    string // resolved username (basic auth)
	AuthHeaderName string // header name (header auth)

	// Response parsing
	ResponseType string // "object" or "array" (default: object)
	ResponsePath string // dot-path into response body

	// Field extraction
	ApprovedField  string
	ApprovedValue  string
	EmergencyField string
	EmergencyValue string
	IdentifierField string

	ChangeID string // resolved change identifier
}

func (c *CustomChecker) CheckRFC() (*CheckResult, error) {
	// If the URL contains {{change_id}} and no change ID is provided, fail
	if c.ChangeID == "" && strings.Contains(c.URL, "{{change_id}}") {
		return &CheckResult{
			Approved: false,
			Message:  "no change ID provided for custom RFC check",
		}, nil
	}

	// Substitute {{change_id}} in URL
	reqURL := c.URL
	if c.ChangeID != "" {
		reqURL = strings.ReplaceAll(reqURL, "{{change_id}}", url.PathEscape(c.ChangeID))
	}

	method := c.Method
	if method == "" {
		method = http.MethodGet
	}

	req, err := http.NewRequest(method, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	applyAuth(req, c.AuthType, c.AuthToken, c.AuthUsername, c.AuthHeaderName)

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying custom RFC API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("custom RFC API returned %d: %s", resp.StatusCode, string(body))
	}

	var parsed interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	// Apply response_path
	if c.ResponsePath != "" {
		parsed, err = extractField(parsed, c.ResponsePath)
		if err != nil {
			return nil, fmt.Errorf("extracting response_path %q: %w", c.ResponsePath, err)
		}
	}

	// Handle array response type
	if c.ResponseType == "array" {
		arr, ok := parsed.([]interface{})
		if !ok {
			return nil, fmt.Errorf("expected array response, got %T", parsed)
		}
		if len(arr) == 0 {
			return &CheckResult{
				Approved:      false,
				RFCIdentifier: c.ChangeID,
				Message:       "no results returned from custom RFC API",
			}, nil
		}
		parsed = arr[0]
	}

	// Extract approved field
	approvedVal, err := extractStringField(parsed, c.ApprovedField)
	if err != nil {
		return nil, fmt.Errorf("extracting approved_field %q: %w", c.ApprovedField, err)
	}

	approved := strings.EqualFold(approvedVal, c.ApprovedValue)

	// Extract optional emergency field
	emergency := false
	if c.EmergencyField != "" && c.EmergencyValue != "" {
		emergencyVal, err := extractStringField(parsed, c.EmergencyField)
		if err == nil {
			emergency = strings.EqualFold(emergencyVal, c.EmergencyValue)
		}
	}

	// Extract optional identifier field
	identifier := c.ChangeID
	if c.IdentifierField != "" {
		if id, err := extractStringField(parsed, c.IdentifierField); err == nil && id != "" {
			identifier = id
		}
	}

	if !approved {
		return &CheckResult{
			Approved:      false,
			Emergency:     emergency,
			RFCIdentifier: identifier,
			Message:       fmt.Sprintf("change %s has %s=%q (expected %q)", identifier, c.ApprovedField, approvedVal, c.ApprovedValue),
		}, nil
	}

	return &CheckResult{
		Approved:      true,
		Emergency:     emergency,
		RFCIdentifier: identifier,
		Message:       fmt.Sprintf("change %s is approved", identifier),
	}, nil
}

// applyAuth sets authentication headers on the request based on the auth type.
func applyAuth(req *http.Request, authType, token, username, headerName string) {
	switch authType {
	case "bearer", "id_token":
		req.Header.Set("Authorization", "Bearer "+token)
	case "basic":
		req.SetBasicAuth(username, token)
	case "header":
		req.Header.Set(headerName, token)
	}
}
