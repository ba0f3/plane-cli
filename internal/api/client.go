package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type HTTPError struct {
	StatusCode int
	Method     string
	URL        string
	Body       string
}

func (e *HTTPError) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > 500 {
		body = body[:500] + "…"
	}
	if body == "" {
		return fmt.Sprintf("Plane API %s %s returned HTTP %d", e.Method, e.URL, e.StatusCode)
	}
	return fmt.Sprintf("Plane API %s %s returned HTTP %d: %s", e.Method, e.URL, e.StatusCode, body)
}

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	UserAgent  string
}

func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Token:   strings.TrimSpace(token),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		UserAgent: "plane-cli/dev",
	}
}

func (c *Client) Request(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", c.Token)
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{StatusCode: resp.StatusCode, Method: method, URL: u, Body: string(payload)}
	}
	if out == nil || len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode Plane API response: %w", err)
	}
	return nil
}

func (c *Client) AuthContext(ctx context.Context) (AuthContext, error) {
	var out AuthContext
	err := c.Request(ctx, http.MethodGet, "/api/v1/auth/context/", nil, nil, &out)
	return out, err
}

func (c *Client) Workspaces(ctx context.Context) ([]Workspace, error) {
	var out []Workspace
	err := c.Request(ctx, http.MethodGet, "/api/v1/workspaces/", nil, nil, &out)
	return out, err
}

func (c *Client) ListObjects(ctx context.Context, path string, query url.Values) ([]Object, error) {
	if query == nil {
		query = url.Values{}
	} else {
		query = cloneValues(query)
	}
	if query.Get("per_page") == "" {
		query.Set("per_page", "100")
	}

	var all []Object
	seen := map[string]bool{}
	for page := 0; page < 10000; page++ {
		var raw json.RawMessage
		if err := c.Request(ctx, http.MethodGet, path, query, nil, &raw); err != nil {
			return nil, err
		}
		var list []Object
		if err := json.Unmarshal(raw, &list); err == nil {
			all = append(all, list...)
			return all, nil
		}

		var env struct {
			Results         []Object `json:"results"`
			NextCursor      any      `json:"next_cursor"`
			NextPageResults bool     `json:"next_page_results"`
			TotalPages      int      `json:"total_pages"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("unexpected list response from %s", path)
		}
		all = append(all, env.Results...)
		if !env.NextPageResults || env.NextCursor == nil {
			return all, nil
		}
		cursor := cursorString(env.NextCursor)
		if cursor == "" || seen[cursor] {
			return all, nil
		}
		seen[cursor] = true
		query.Set("cursor", cursor)
	}
	return nil, errors.New("pagination exceeded safety limit")
}

func (c *Client) GetObject(ctx context.Context, path string) (Object, error) {
	var out Object
	err := c.Request(ctx, http.MethodGet, path, nil, nil, &out)
	return out, err
}

func (c *Client) WriteObject(ctx context.Context, method, path string, body any) (Object, error) {
	var out Object
	err := c.Request(ctx, method, path, nil, body, &out)
	return out, err
}

func cursorString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case json.Number:
		return x.String()
	default:
		b, _ := json.Marshal(x)
		return strings.Trim(string(b), `"`)
	}
}

func cloneValues(in url.Values) url.Values {
	out := make(url.Values, len(in))
	for k, values := range in {
		out[k] = append([]string(nil), values...)
	}
	return out
}
