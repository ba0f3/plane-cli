package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rohithmahesh3/plane-cli/internal/config"
)

const (
	DefaultTimeout = 30 * time.Second
	APIVersion     = "v1"
)

type Client struct {
	HTTPClient *http.Client
	BaseURL    string
	APIKey     string
	Workspace  string
}

type Pagination struct {
	NextCursor      string `json:"next_cursor"`
	PrevCursor      string `json:"prev_cursor"`
	NextPageResults bool   `json:"next_page_results"`
	PrevPageResults bool   `json:"prev_page_results"`
	Count           int    `json:"count"`
	TotalPages      int    `json:"total_pages"`
	TotalResults    int    `json:"total_results"`
}

type Response struct {
	Pagination
	Results json.RawMessage `json:"results"`
}

// NewClientNoWorkspace creates an authenticated client without requiring a
// workspace. Use this for instance-level discovery and reporting.
func NewClientNoWorkspace() (*Client, error) {
	apiKey, err := config.GetAPIKey()
	if err != nil || strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("not authenticated. Run 'plane-cli auth login' first or set PLANE_API_KEY")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(config.Cfg.APIHost), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("no API host configured. Run 'plane-cli auth login' or set PLANE_API_HOST")
	}

	return &Client{
		HTTPClient: &http.Client{Timeout: DefaultTimeout},
		BaseURL:    baseURL,
		APIKey:     apiKey,
		Workspace:  strings.TrimSpace(config.Cfg.DefaultWorkspace),
	}, nil
}

// NewClient creates a workspace-bound client. WSAT automatically resolves its
// bound workspace. IAT intentionally requires an explicit/default workspace for
// commands that mutate or otherwise operate on one workspace.
func NewClient() (*Client, error) {
	client, err := NewClientNoWorkspace()
	if err != nil {
		return nil, err
	}
	if client.Workspace != "" {
		return client, nil
	}

	ctx, err := client.GetAuthContext()
	if err == nil && ctx.Workspace != nil && ctx.Workspace.Slug != "" {
		client.Workspace = ctx.Workspace.Slug
		return client, nil
	}

	return nil, fmt.Errorf("no workspace selected for this command. Use --workspace <slug> or set PLANE_WORKSPACE/default_workspace")
}

func (c *Client) NewRequest(method, path string, body interface{}) (*http.Request, error) {
	urlStr := fmt.Sprintf("%s/api/%s%s", c.BaseURL, APIVersion, path)

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, urlStr, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-API-Key", c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	return req, nil
}

func (c *Client) Do(req *http.Request, v interface{}) error {
	body, err := c.DoRaw(req)
	if err != nil {
		return err
	}
	if v != nil && len(body) > 0 {
		return json.Unmarshal(body, v)
	}
	return nil
}

func (c *Client) Get(path string, query url.Values, v interface{}) error {
	if query != nil {
		path = path + "?" + query.Encode()
	}
	req, err := c.NewRequest("GET", path, nil)
	if err != nil {
		return err
	}
	return c.Do(req, v)
}

func (c *Client) DoRaw(req *http.Request) ([]byte, error) {
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *Client) GetRaw(path string, query url.Values) ([]byte, error) {
	if query != nil {
		path = path + "?" + query.Encode()
	}
	req, err := c.NewRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	return c.DoRaw(req)
}

func (c *Client) Post(path string, body interface{}, v interface{}) error {
	req, err := c.NewRequest("POST", path, body)
	if err != nil {
		return err
	}
	return c.Do(req, v)
}

func (c *Client) Patch(path string, body interface{}, v interface{}) error {
	req, err := c.NewRequest("PATCH", path, body)
	if err != nil {
		return err
	}
	return c.Do(req, v)
}

func (c *Client) Delete(path string) error {
	req, err := c.NewRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	return c.Do(req, nil)
}

func (c *Client) SetWorkspace(workspace string) { c.Workspace = workspace }

func (c *Client) ForWorkspace(workspace string) *Client {
	clone := *c
	clone.Workspace = workspace
	return &clone
}

func unmarshalListResponse[T any](body []byte) ([]T, error) {
	var wrapped struct {
		Results json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Results != nil {
		var items []T
		if err := json.Unmarshal(wrapped.Results, &items); err != nil {
			return nil, err
		}
		return items, nil
	}
	var items []T
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func isAPIStatusError(err error, statusCode int) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), fmt.Sprintf("status %d", statusCode))
}
