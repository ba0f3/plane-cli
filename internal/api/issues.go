package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/rohithmahesh3/plane-cli/pkg/plane"
)

type IssueListOptions struct {
	State    string // State UUID or name (resolved by caller for consistent behavior)
	Assignee string // Assignee UUID or "me" (resolved by caller for consistent behavior)
	Limit    int
	Offset   int
}

func (c *Client) ListIssues(projectID string, opts IssueListOptions) ([]plane.Issue, *Pagination, error) {
	path := fmt.Sprintf("/workspaces/%s/projects/%s/work-items/", c.Workspace, projectID)

	query := url.Values{}
	query.Set("expand", "assignees,state")
	if opts.State != "" {
		query.Set("state", opts.State)
	}
	if opts.Assignee != "" {
		query.Set("assignee", opts.Assignee)
	}
	if opts.Limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Offset > 0 {
		query.Set("offset", fmt.Sprintf("%d", opts.Offset))
	}

	var response Response
	if err := c.Get(path, query, &response); err != nil {
		return nil, nil, err
	}

	var issues []plane.Issue
	if err := json.Unmarshal(response.Results, &issues); err != nil {
		return nil, nil, err
	}

	return issues, &response.Pagination, nil
}

func (c *Client) ListAllIssues(projectID string) ([]plane.Issue, error) {
	const pageSize = 100
	var all []plane.Issue
	offset := 0
	for {
		items, page, err := c.ListIssues(projectID, IssueListOptions{Limit: pageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			break
		}
		all = append(all, items...)
		offset += len(items)

		if page != nil {
			if page.TotalResults > 0 && offset >= page.TotalResults {
				break
			}
			if !page.NextPageResults && len(items) < pageSize {
				break
			}
		} else if len(items) < pageSize {
			break
		}

		// Defensive stop if an API ignores offset and keeps returning the same page.
		if offset > 1000000 {
			return nil, fmt.Errorf("work-item pagination exceeded safety limit")
		}
	}
	return all, nil
}

func (c *Client) GetIssue(projectID, issueID string) (*plane.Issue, error) {
	path := fmt.Sprintf("/workspaces/%s/projects/%s/work-items/%s/", c.Workspace, projectID, issueID)

	query := url.Values{}
	query.Set("expand", "assignees,state,labels")

	var issue plane.Issue
	if err := c.Get(path, query, &issue); err != nil {
		return nil, err
	}

	return &issue, nil
}

func (c *Client) GetIssueByIdentifier(identifier string) (*plane.Issue, error) {
	path := fmt.Sprintf("/workspaces/%s/work-items/%s/", c.Workspace, url.PathEscape(identifier))

	query := url.Values{}
	query.Set("expand", "assignees,state,labels")

	var issue plane.Issue
	if err := c.Get(path, query, &issue); err != nil {
		return nil, err
	}

	return &issue, nil
}

func (c *Client) GetIssueBySequenceID(projectID string, sequenceID int) (*plane.Issue, error) {
	project, err := c.GetProject(projectID)
	if err != nil {
		return nil, err
	}

	identifier := strings.TrimSpace(project.Identifier)
	if identifier == "" {
		return nil, fmt.Errorf("project %s has no identifier", projectID)
	}

	return c.GetIssueByIdentifier(fmt.Sprintf("%s-%d", identifier, sequenceID))
}

func (c *Client) CreateIssue(projectID string, req plane.CreateIssueRequest) (*plane.Issue, error) {
	path := fmt.Sprintf("/workspaces/%s/projects/%s/work-items/", c.Workspace, projectID)

	var issue plane.Issue
	if err := c.Post(path, req, &issue); err != nil {
		return nil, err
	}

	return &issue, nil
}

func (c *Client) UpdateIssue(projectID, issueID string, req plane.UpdateIssueRequest) (*plane.Issue, error) {
	path := fmt.Sprintf("/workspaces/%s/projects/%s/work-items/%s/", c.Workspace, projectID, issueID)

	var issue plane.Issue
	if err := c.Patch(path, req, &issue); err != nil {
		return nil, err
	}

	return &issue, nil
}

func (c *Client) DeleteIssue(projectID, issueID string) error {
	path := fmt.Sprintf("/workspaces/%s/projects/%s/work-items/%s/", c.Workspace, projectID, issueID)
	return c.Delete(path)
}

// SearchIssues searches for issues across the workspace
// Endpoint: GET /api/v1/workspaces/{workspace_slug}/work-items/search/
// Returns: {"issues": [...]}
func (c *Client) SearchIssues(query string) ([]plane.Issue, error) {
	path := fmt.Sprintf("/workspaces/%s/work-items/search/", c.Workspace)

	params := url.Values{}
	params.Set("search", query)

	// The search endpoint returns a different structure
	var response struct {
		Issues []plane.Issue `json:"issues"`
	}

	if err := c.Get(path, params, &response); err != nil {
		return nil, err
	}

	return response.Issues, nil
}
