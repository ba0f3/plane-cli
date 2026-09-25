package api

import (
	"fmt"
	"strings"

	"github.com/rohithmahesh3/plane-cli/pkg/plane"
)

func (c *Client) GetWorkspace(slug string) (*WorkspaceSummary, error) {
	workspaces, err := c.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	for i := range workspaces {
		if strings.EqualFold(workspaces[i].Slug, slug) || workspaces[i].ID == slug {
			return &workspaces[i], nil
		}
	}
	return nil, fmt.Errorf("workspace %q not found or not visible to this token", slug)
}

// GetUserInfo retrieves the current authenticated user info.
// Service-token callers should prefer GetAuthContext.
func (c *Client) GetUserInfo() (*plane.User, error) {
	var user plane.User
	if err := c.Get("/users/me/", nil, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (c *Client) GetWorkspaceMembers() ([]plane.User, error) {
	if strings.TrimSpace(c.Workspace) == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	path := fmt.Sprintf("/workspaces/%s/members/", c.Workspace)
	var members []plane.User
	if err := c.Get(path, nil, &members); err != nil {
		return nil, err
	}
	return members, nil
}
