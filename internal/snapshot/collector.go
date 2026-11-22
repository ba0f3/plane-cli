package snapshot

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/ba0f3/plane-cli/pkg/plane"
)

const (
	defaultPageSize     = 100
	maxPagesSafetyLimit = 10000
	maxRetries          = 3
	maxRetryDelay       = 30 * time.Second
)

type projectRef struct {
	Workspace  string
	ID         string
	Identifier string
}

type rawPage struct {
	Results         []json.RawMessage `json:"results"`
	NextPageResults *bool             `json:"next_page_results"`
	NextCursor      string            `json:"next_cursor"`
}

func resolveProjects(scope Scope, client *api.Client) ([]projectRef, error) {
	var workspaces []string
	if scope.AllWorkspaces {
		if strings.TrimSpace(scope.Workspace) != "" {
			return nil, fmt.Errorf("--all-workspaces cannot be combined with --workspace")
		}
		discovered, err := client.ListWorkspaces()
		if err != nil {
			return nil, fmt.Errorf("list workspaces: %w", err)
		}
		for _, workspace := range discovered {
			workspaces = append(workspaces, workspace.Slug)
		}
	} else {
		workspace := scope.Workspace
		if workspace == "" {
			workspace = config.Cfg.DefaultWorkspace
		}
		if workspace == "" {
			return nil, fmt.Errorf("no workspace selected: use --workspace or set PLANE_WORKSPACE")
		}
		workspaces = []string{workspace}
	}

	var refs []projectRef
	for _, workspace := range workspaces {
		scoped := client.CloneForWorkspace(workspace)
		projects, err := scoped.ListProjects()
		if err != nil {
			return nil, fmt.Errorf("workspace %s: list projects: %w", workspace, err)
		}
		for _, project := range projects {
			if len(scope.Projects) > 0 && !matchesAny(project, scope.Projects) {
				continue
			}
			refs = append(refs, projectRef{Workspace: workspace, ID: project.ID, Identifier: project.Identifier})
		}
	}
	return refs, nil
}

func matchesAny(project plane.Project, selectors []string) bool {
	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		if strings.EqualFold(project.ID, selector) ||
			strings.EqualFold(project.Identifier, selector) ||
			strings.EqualFold(project.Name, selector) {
			return true
		}
	}
	return false
}

func retryDelay(header string, attempt int) time.Duration {
	if trimmed := strings.TrimSpace(header); trimmed != "" {
		if seconds, err := strconv.Atoi(trimmed); err == nil && seconds > 0 {
			if seconds > int(maxRetryDelay/time.Second) {
				seconds = int(maxRetryDelay / time.Second)
			}
			return time.Duration(seconds) * time.Second
		}
	}
	return time.Duration(1<<attempt) * time.Second
}

func fetchPage(client *api.Client, ref projectRef, expand, cursor string, sleep func(time.Duration)) (rawPage, error) {
	var page rawPage
	path := fmt.Sprintf("/workspaces/%s/projects/%s/work-items/",
		url.PathEscape(ref.Workspace), url.PathEscape(ref.ID))
	query := url.Values{}
	query.Set("expand", expand)
	query.Set("per_page", strconv.Itoa(defaultPageSize))
	if cursor != "" {
		query.Set("cursor", cursor)
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := client.NewRequest(http.MethodGet, path+"?"+query.Encode(), nil)
		if err != nil {
			return page, err
		}
		resp, err := client.HTTPClient.Do(req)
		if err != nil {
			return page, err
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return page, readErr
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxRetries {
			sleep(retryDelay(resp.Header.Get("Retry-After"), attempt))
			continue
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return page, fmt.Errorf("API error (status %d): check token scope; body: %s", resp.StatusCode, string(body))
		}
		if resp.StatusCode >= 400 {
			return page, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return page, fmt.Errorf("decode work-item page: %w", err)
		}
		if page.NextPageResults == nil {
			return page, fmt.Errorf("missing next_page_results in %s/%s response", ref.Workspace, ref.Identifier)
		}
		return page, nil
	}
	return page, fmt.Errorf("exceeded %d retries for %s/%s", maxRetries, ref.Workspace, ref.Identifier)
}

func collectProject(client *api.Client, ref projectRef, expand string, byID map[string]Record, sleep func(time.Duration)) (ProjectCoverage, error) {
	cov := ProjectCoverage{Workspace: ref.Workspace, ProjectID: ref.ID}
	cursor := ""
	seen := map[string]bool{}

	for pageNumber := 1; pageNumber <= maxPagesSafetyLimit; pageNumber++ {
		page, err := fetchPage(client, ref, expand, cursor, sleep)
		if err != nil {
			return cov, err
		}
		cov.Pages = pageNumber
		cov.Items += len(page.Results)

		for _, item := range page.Results {
			var meta struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(item, &meta); err != nil {
				return cov, fmt.Errorf("decode item id on page %d: %w", pageNumber, err)
			}
			if meta.ID == "" {
				return cov, fmt.Errorf("item without id on page %d", pageNumber)
			}
			key := ref.Workspace + "/" + ref.ID + "/" + meta.ID
			if _, exists := byID[key]; exists {
				cov.Duplicates++
				continue
			}
			byID[key] = Record{
				Workspace: ref.Workspace, ProjectID: ref.ID,
				ProjectIdentifier: ref.Identifier, Item: item,
			}
		}

		if !*page.NextPageResults {
			cov.Retained = cov.Items - cov.Duplicates
			return cov, nil
		}
		cursor = page.NextCursor
		if cursor == "" || seen[cursor] {
			return cov, fmt.Errorf("missing or repeated next cursor on page %d", pageNumber)
		}
		seen[cursor] = true
	}
	return cov, fmt.Errorf("pagination safety limit reached for %s/%s", ref.Workspace, ref.Identifier)
}

// Collect walks every selected project and returns one complete snapshot, or
// an error. It never returns partial data as a complete snapshot.
func Collect(scope Scope) (*Snapshot, error) {
	started := time.Now().UTC().Format(time.RFC3339)
	client, err := api.NewClientNoWorkspace()
	if err != nil {
		return nil, err
	}
	refs, err := resolveProjects(scope, client)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]Record)
	var coverage []ProjectCoverage
	for _, ref := range refs {
		cov, err := collectProject(client, ref, scope.Expand, byID, time.Sleep)
		if err != nil {
			return nil, fmt.Errorf("workspace %s project %s: %w", ref.Workspace, ref.Identifier, err)
		}
		coverage = append(coverage, cov)
	}

	records := make([]Record, 0, len(byID))
	for _, record := range byID {
		records = append(records, record)
	}
	records, err = FilterRecords(records, scope, time.Now())
	if err != nil {
		return nil, err
	}

	retained := make(map[string]int)
	for _, record := range records {
		retained[record.Workspace+"/"+record.ProjectID]++
	}
	for i := range coverage {
		coverage[i].Retained = retained[coverage[i].Workspace+"/"+coverage[i].ProjectID]
	}

	return &Snapshot{
		StartedAt: started, FinishedAt: time.Now().UTC().Format(time.RFC3339),
		Scope: scope, Coverage: coverage, Records: records,
	}, nil
}
