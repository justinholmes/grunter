package rfc

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/justinholmes/grunter/internal/config"
)

// ServiceNowChecker verifies RFC approval by querying the ServiceNow change_request table.
type ServiceNowChecker struct {
	HTTPClient   *http.Client
	InstanceURL  string
	Username     string
	Password     string
	ChangeNumber string
	Config       *config.RFCConfig
}

type snowResult struct {
	Result []snowChangeRequest `json:"result"`
}

type snowChangeRequest struct {
	Number   string `json:"number"`
	Approval string `json:"approval"`
	Type     string `json:"type"`
}

func (s *ServiceNowChecker) CheckRFC() (*CheckResult, error) {
	if s.ChangeNumber == "" {
		return &CheckResult{
			Approved: false,
			Message:  fmt.Sprintf("no change number provided (set --%s or %s env var)", "change-number", s.Config.ChangeNumberVar),
		}, nil
	}

	endpoint := fmt.Sprintf("%s/api/now/table/change_request?sysparm_query=number=%s&sysparm_limit=1",
		strings.TrimRight(s.InstanceURL, "/"), url.QueryEscape(s.ChangeNumber))

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.SetBasicAuth(s.Username, s.Password)
	req.Header.Set("Accept", "application/json")

	client := s.HTTPClient
	if client == nil {
		client = &http.Client{}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying ServiceNow: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ServiceNow API returned %d: %s", resp.StatusCode, string(body))
	}

	var result snowResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(result.Result) == 0 {
		return &CheckResult{
			Approved:      false,
			RFCIdentifier: s.ChangeNumber,
			Message:       fmt.Sprintf("change request %s not found in ServiceNow", s.ChangeNumber),
		}, nil
	}

	cr := result.Result[0]
	approved := cr.Approval == "approved"
	emergency := s.Config.EmergencyLabel != "" && cr.Type == s.Config.EmergencyLabel

	if !approved {
		return &CheckResult{
			Approved:      false,
			Emergency:     emergency,
			RFCIdentifier: s.ChangeNumber,
			Message:       fmt.Sprintf("change request %s has approval=%q (expected \"approved\")", s.ChangeNumber, cr.Approval),
		}, nil
	}

	return &CheckResult{
		Approved:      true,
		Emergency:     emergency,
		RFCIdentifier: s.ChangeNumber,
		Message:       fmt.Sprintf("change request %s is approved", s.ChangeNumber),
	}, nil
}
