package api

import (
	"encoding/json"
	"fmt"
)

type WorkspaceSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type AuthContext struct {
	PrincipalType string            `json:"principal_type"`
	ScopeLevel    string            `json:"scope_level"`
	IsService     bool              `json:"is_service"`
	Workspace     *WorkspaceSummary `json:"workspace"`
	Scopes        []string          `json:"scopes"`
}

func (c *Client) GetAuthContext() (*AuthContext, error) {
	var out AuthContext
	if err := c.Get("/auth/context/", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListWorkspaces() ([]WorkspaceSummary, error) {
	body, err := c.GetRaw("/workspaces/", nil)
	if err != nil {
		return nil, err
	}

	var direct []WorkspaceSummary
	if err := json.Unmarshal(body, &direct); err == nil {
		return direct, nil
	}

	var wrapped struct {
		Results []WorkspaceSummary `json:"results"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, fmt.Errorf("decode workspace discovery response: %w", err)
	}
	return wrapped.Results, nil
}
