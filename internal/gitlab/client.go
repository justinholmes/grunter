package gitlab

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const markerPrefix = "<!-- grunter:"
const markerSuffix = " -->"

// Client interacts with the GitLab REST API v4.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a GitLab API client.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: &http.Client{},
	}
}

type note struct {
	ID   int    `json:"id"`
	Body string `json:"body"`
}

// PostOrUpdateComment posts a comment on a MR, or updates an existing one
// identified by a marker for the given unit path.
func (c *Client) PostOrUpdateComment(projectID, mrIID, unitPath, body string) error {
	marker := markerPrefix + unitPath + markerSuffix
	markedBody := marker + "\n" + body

	existing, err := c.findExistingNote(projectID, mrIID, marker)
	if err != nil {
		return fmt.Errorf("searching existing notes: %w", err)
	}

	if existing != nil {
		return c.updateNote(projectID, mrIID, existing.ID, markedBody)
	}
	return c.createNote(projectID, mrIID, markedBody)
}

// SetCommitStatus sets a commit status on the given SHA.
func (c *Client) SetCommitStatus(projectID, sha, state, name, description, targetURL string) error {
	endpoint := fmt.Sprintf("%s/api/v4/projects/%s/statuses/%s",
		c.baseURL, url.PathEscape(projectID), url.PathEscape(sha))

	payload := map[string]string{
		"state":       state,
		"name":        name,
		"description": description,
	}
	if targetURL != "" {
		payload["target_url"] = targetURL
	}

	return c.doJSON(http.MethodPost, endpoint, payload, nil)
}

func (c *Client) findExistingNote(projectID, mrIID, marker string) (*note, error) {
	endpoint := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%s/notes?per_page=100",
		c.baseURL, url.PathEscape(projectID), url.PathEscape(mrIID))

	var notes []note
	if err := c.doJSON(http.MethodGet, endpoint, nil, &notes); err != nil {
		return nil, err
	}

	for i := range notes {
		if strings.Contains(notes[i].Body, marker) {
			return &notes[i], nil
		}
	}
	return nil, nil
}

func (c *Client) createNote(projectID, mrIID, body string) error {
	endpoint := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%s/notes",
		c.baseURL, url.PathEscape(projectID), url.PathEscape(mrIID))

	payload := map[string]string{"body": body}
	return c.doJSON(http.MethodPost, endpoint, payload, nil)
}

func (c *Client) updateNote(projectID, mrIID string, noteID int, body string) error {
	endpoint := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%s/notes/%d",
		c.baseURL, url.PathEscape(projectID), url.PathEscape(mrIID), noteID)

	payload := map[string]string{"body": body}
	return c.doJSON(http.MethodPut, endpoint, payload, nil)
}

func (c *Client) doJSON(method, endpoint string, payload interface{}, result interface{}) error {
	var bodyReader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, endpoint, bodyReader)
	if err != nil {
		return err
	}

	req.Header.Set("PRIVATE-TOKEN", c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("GitLab API %s %s returned %d: %s", method, endpoint, resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}
