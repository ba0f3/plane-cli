package snapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
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

func resolveProjects(scope Scope, client *api.Client, sleep func(time.Duration)) ([]projectRef, error) {
	var workspaces []string
	if scope.AllWorkspaces {
		if strings.TrimSpace(scope.Workspace) != "" {
			return nil, fmt.Errorf("--all-workspaces cannot be combined with --workspace")
		}
		var discovered []api.WorkspaceSummary
		if err := retryDiscovery("list workspaces", sleep, func() error {
			var err error
			discovered, err = client.ListWorkspaces()
			return err
		}); err != nil {
			return nil, err
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
		var projects []plane.Project
		if err := retryDiscovery(fmt.Sprintf("workspace %s: list projects", workspace), sleep, func() error {
			var err error
			projects, err = scoped.ListProjects()
			return err
		}); err != nil {
			return nil, err
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

// retryDiscovery applies the same bounded 429 backoff and 401/403 token-scope
// policy to instance and project discovery calls that fetchPage applies to
// work-item pages.
func retryDiscovery(what string, sleep func(time.Duration), fn func() error) error {
	for attempt := 0; ; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		var statusErr *api.HTTPStatusError
		if !errors.As(err, &statusErr) {
			return fmt.Errorf("%s: %w", what, err)
		}
		switch statusErr.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("%s: API error (status %d): check token scope; body: %s",
				what, statusErr.Status, string(statusErr.Body))
		case http.StatusTooManyRequests:
			if attempt >= maxRetries {
				return fmt.Errorf("%s: %w", what, err)
			}
			sleep(retryDelayWithJitter("", attempt))
		default:
			return fmt.Errorf("%s: %w", what, err)
		}
	}
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
	delay := time.Duration(1<<attempt) * time.Second
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	return delay
}

// retryDelayWithJitter spreads retries across [base/2, base] so that parallel
// clients do not retry in lockstep after a shared 429.
func retryDelayWithJitter(header string, attempt int) time.Duration {
	base := retryDelay(header, attempt)
	if base <= 1 {
		return base
	}
	return base/2 + time.Duration(rand.Int63n(int64(base/2)+1))
}

// decodePage validates the pagination envelope. A response without a results
// array is an error, never an empty page.
func decodePage(body []byte, ref projectRef) (rawPage, error) {
	var envelope struct {
		Results         json.RawMessage `json:"results"`
		NextPageResults *bool           `json:"next_page_results"`
		NextCursor      string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return rawPage{}, fmt.Errorf("decode work-item page: %w", err)
	}
	if envelope.Results == nil {
		return rawPage{}, fmt.Errorf("missing results in %s/%s response", ref.Workspace, ref.Identifier)
	}
	if bytes.Equal(bytes.TrimSpace(envelope.Results), []byte("null")) {
		return rawPage{}, fmt.Errorf("results must be an array in %s/%s response", ref.Workspace, ref.Identifier)
	}
	var results []json.RawMessage
	if err := json.Unmarshal(envelope.Results, &results); err != nil {
		return rawPage{}, fmt.Errorf("results must be an array in %s/%s response: %w", ref.Workspace, ref.Identifier, err)
	}
	if envelope.NextPageResults == nil {
		return rawPage{}, fmt.Errorf("missing next_page_results in %s/%s response", ref.Workspace, ref.Identifier)
	}
	return rawPage{
		Results:         results,
		NextPageResults: envelope.NextPageResults,
		NextCursor:      envelope.NextCursor,
	}, nil
}

func fetchPage(client *api.Client, ref projectRef, expand, cursor string, sleep func(time.Duration)) (rawPage, error) {
	path := fmt.Sprintf("/workspaces/%s/projects/%s/work-items/",
		url.PathEscape(ref.Workspace), url.PathEscape(ref.ID))
	query := url.Values{}
	query.Set("expand", expand)
	query.Set("per_page", strconv.Itoa(defaultPageSize))
	if cursor != "" {
		query.Set("cursor", cursor)
	}

	for attempt := 0; ; attempt++ {
		status, headers, body, err := client.GetRawWithStatus(path, query)
		if err != nil {
			return rawPage{}, err
		}

		if status == http.StatusTooManyRequests {
			if attempt >= maxRetries {
				return rawPage{}, &api.HTTPStatusError{Status: status, Body: body}
			}
			sleep(retryDelayWithJitter(headers.Get("Retry-After"), attempt))
			continue
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return rawPage{}, fmt.Errorf("API error (status %d): check token scope; body: %s", status, string(body))
		}
		if status >= 400 {
			return rawPage{}, &api.HTTPStatusError{Status: status, Body: body}
		}
		return decodePage(body, ref)
	}
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
	refs, err := resolveProjects(scope, client, time.Sleep)
	if err != nil {
		return nil, err
	}
	if len(scope.Projects) > 0 && len(refs) == 0 {
		return nil, fmt.Errorf("no projects matched selector(s): %s", strings.Join(scope.Projects, ", "))
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
