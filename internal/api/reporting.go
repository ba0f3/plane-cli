package api

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/ba0f3/plane-cli/pkg/plane"
)

// ListAllIssues walks Plane's cursor pagination and returns all work items
// in the client's current workspace/project scope.
func (c *Client) ListAllIssues(projectID string) ([]plane.Issue, error) {
	if c.Workspace == "" {
		return nil, fmt.Errorf("workspace is required to list project work items")
	}
	path := fmt.Sprintf("/workspaces/%s/projects/%s/work-items/", c.Workspace, projectID)
	query := url.Values{}
	query.Set("expand", "assignees,state,labels")
	query.Set("per_page", "100")

	var all []plane.Issue
	seen := map[string]bool{}
	for page := 0; page < 10000; page++ {
		body, err := c.GetRaw(path, query)
		if err != nil {
			return nil, err
		}

		var direct []plane.Issue
		if err := json.Unmarshal(body, &direct); err == nil {
			all = append(all, direct...)
			return all, nil
		}

		var response Response
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("decode work-item list: %w", err)
		}
		var items []plane.Issue
		if err := json.Unmarshal(response.Results, &items); err != nil {
			return nil, fmt.Errorf("decode work-item results: %w", err)
		}
		all = append(all, items...)

		if !response.NextPageResults || response.NextCursor == "" {
			return all, nil
		}
		if seen[response.NextCursor] {
			return all, nil
		}
		seen[response.NextCursor] = true
		query.Set("cursor", response.NextCursor)
	}
	return nil, fmt.Errorf("work-item pagination exceeded safety limit")
}
