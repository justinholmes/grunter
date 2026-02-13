package rfc

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CustomWindowChecker verifies deployment windows by querying a user-configured HTTP API.
type CustomWindowChecker struct {
	HTTPClient *http.Client
	URL        string
	Method     string // default: GET

	// Auth
	AuthType       string
	AuthToken      string
	AuthUsername    string
	AuthHeaderName string

	// Response parsing
	ResponsePath    string
	AllowedField    string
	MessageField    string
	NextWindowField string
}

// CheckWindow queries the custom API and returns whether deployment is currently allowed.
func (c *CustomWindowChecker) CheckWindow() (*WindowResult, error) {
	method := c.Method
	if method == "" {
		method = http.MethodGet
	}

	req, err := http.NewRequest(method, c.URL, nil)
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
		return nil, fmt.Errorf("querying deployment window API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("deployment window API returned %d: %s", resp.StatusCode, string(body))
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

	// Extract allowed field — handle both bool and string
	allowedRaw, err := extractField(parsed, c.AllowedField)
	if err != nil {
		return nil, fmt.Errorf("extracting allowed_field %q: %w", c.AllowedField, err)
	}

	allowed, err := toBool(allowedRaw)
	if err != nil {
		return nil, fmt.Errorf("interpreting allowed_field %q: %w", c.AllowedField, err)
	}

	if allowed {
		return &WindowResult{
			Allowed: true,
			Message: "deployment window is open",
		}, nil
	}

	// Not allowed — extract optional message and next window
	result := &WindowResult{
		Allowed: false,
		Message: "deployment window is closed",
	}

	if c.MessageField != "" {
		if msg, err := extractStringField(parsed, c.MessageField); err == nil && msg != "" {
			result.Message = msg
		}
	}

	if c.NextWindowField != "" {
		if nextStr, err := extractStringField(parsed, c.NextWindowField); err == nil && nextStr != "" {
			if t, err := time.Parse(time.RFC3339, nextStr); err == nil {
				result.NextWindowStart = &t
			}
		}
	}

	return result, nil
}

// toBool converts a JSON value (bool or string) to a Go bool.
func toBool(v interface{}) (bool, error) {
	switch val := v.(type) {
	case bool:
		return val, nil
	case string:
		switch val {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return false, fmt.Errorf("expected \"true\" or \"false\", got %q", val)
		}
	default:
		return false, fmt.Errorf("expected bool or string, got %T", v)
	}
}
