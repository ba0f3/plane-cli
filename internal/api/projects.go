package api

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/ba0f3/plane-cli/pkg/plane"
)

func (c *Client) ListProjects() ([]plane.Project, error) {
	if c.Workspace == "" {
		return nil, fmt.Errorf("workspace is required to list projects")
	}

	path := fmt.Sprintf("/workspaces/%s/projects/", c.Workspace)
	query := url.Values{}
	query.Set("per_page", "100")

	var all []plane.Project
	seen := map[string]bool{}
	for page := 0; page < 10000; page++ {
		body, err := c.GetRaw(path, query)
		if err != nil {
			return nil, err
		}

		// Compatibility with endpoints/upstream versions that return a bare list.
		var direct []plane.Project
		if err := json.Unmarshal(body, &direct); err == nil {
			all = append(all, direct...)
			return all, nil
		}

		var response Response
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("decode project list: %w", err)
		}
		var projects []plane.Project
		if err := json.Unmarshal(response.Results, &projects); err != nil {
			return nil, fmt.Errorf("decode project results: %w", err)
		}
		all = append(all, projects...)

		if !response.NextPageResults || response.NextCursor == "" {
			return all, nil
		}
		if seen[response.NextCursor] {
			return nil, fmt.Errorf("project pagination repeated cursor %q", response.NextCursor)
		}
		seen[response.NextCursor] = true
		query.Set("cursor", response.NextCursor)
	}

	return nil, fmt.Errorf("project pagination exceeded safety limit")
}

func (c *Client) GetProject(projectID string) (*plane.Project, error) {
	path := fmt.Sprintf("/workspaces/%s/projects/%s/", c.Workspace, projectID)

	var project plane.Project
	if err := c.Get(path, nil, &project); err != nil {
		return nil, err
	}

	return &project, nil
}

func (c *Client) CreateProject(req plane.CreateProjectRequest) (*plane.Project, error) {
	path := fmt.Sprintf("/workspaces/%s/projects/", c.Workspace)

	var project plane.Project
	if err := c.Post(path, req, &project); err != nil {
		return nil, err
	}

	return &project, nil
}

func (c *Client) DeleteProject(projectID string) error {
	path := fmt.Sprintf("/workspaces/%s/projects/%s/", c.Workspace, projectID)
	return c.Delete(path)
}

func (c *Client) GetProjectMembers(projectID string) ([]plane.User, error) {
	path := fmt.Sprintf("/workspaces/%s/projects/%s/members/", c.Workspace, projectID)

	var members []plane.User
	if err := c.Get(path, nil, &members); err != nil {
		return nil, err
	}

	return members, nil
}
