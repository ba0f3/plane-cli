package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ba0f3/plane-cli/pkg/plane"
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

// MaxAllIssues is the hard ceiling applied when a caller opts out of result
// truncation. Exceeding it is reported as an error rather than silently
// returning a partial list.
const MaxAllIssues = 500

// allIssuesPageSize is the fixed page size used when walking the full result
// set. At the MaxAllIssues ceiling this costs five requests.
const allIssuesPageSize = 100

// maxPagesSafetyLimit bounds the pagination loop so a server that always
// answers with a full page cannot spin forever.
const maxPagesSafetyLimit = 10000

// ListAllIssuesPaged walks offset pagination for one project and returns every
// work item matching opts' State and Assignee filters. It returns an error,
// and no partial results, if the matching set is larger than max.
//
// Offset pagination is used deliberately: ListIssues already proves that
// limit/offset compose correctly with the state and assignee query parameters,
// whereas cursor pagination combined with those filters is unverified against
// this endpoint.
func (c *Client) ListAllIssuesPaged(projectID string, opts IssueListOptions, max int) ([]plane.Issue, error) {
	path := fmt.Sprintf("/workspaces/%s/projects/%s/work-items/", c.Workspace, projectID)

	if max <= 0 {
		max = MaxAllIssues
	}

	query := url.Values{}
	query.Set("expand", "assignees,state")
	if opts.State != "" {
		query.Set("state", opts.State)
	}
	if opts.Assignee != "" {
		query.Set("assignee", opts.Assignee)
	}
	query.Set("limit", strconv.Itoa(allIssuesPageSize))

	var all []plane.Issue
	// The loop runs while len(all) <= max, not < max: detecting that the result
	// set exceeds the ceiling requires actually fetching the max+1-th item. A
	// short page ends the walk, which is why a set of exactly max costs one
	// extra empty request to confirm it ended.
	for page := 0; len(all) <= max && page < maxPagesSafetyLimit; page++ {
		items, err := c.fetchIssuePage(path, query, len(all))
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(items) < allIssuesPageSize {
			break
		}
	}

	if len(all) > max {
		return nil, fmt.Errorf("more than %d matching issues; use --limit to page through results", max)
	}

	return all, nil
}

// fetchIssuePage requests one offset page of work items, leaving query
// untouched so the caller controls it across iterations.
func (c *Client) fetchIssuePage(path string, query url.Values, offset int) ([]plane.Issue, error) {
	pageQuery := url.Values{}
	for key, values := range query {
		for _, value := range values {
			pageQuery.Add(key, value)
		}
	}
	if offset > 0 {
		pageQuery.Set("offset", strconv.Itoa(offset))
	}

	var response Response
	if err := c.Get(path, pageQuery, &response); err != nil {
		return nil, err
	}

	var items []plane.Issue
	if len(response.Results) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(response.Results, &items); err != nil {
		return nil, fmt.Errorf("decode work-item results: %w", err)
	}
	return items, nil
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
