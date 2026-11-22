# One-shot work-item snapshot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give agents one `plane-cli snapshot` command that collects a complete, detailed work-item dataset in a single CLI process, and point every agent instruction surface at it instead of per-item reads.

**Architecture:** A new `internal/snapshot` package paginates the work-items endpoint with raw JSON preservation, deduplicates, applies an optional local date filter, and returns one `Snapshot`. A thin `cmd/snapshot` Cobra command resolves flags, writes the bulk data atomically to a file, and prints only a small coverage summary. Three instruction surfaces (`skills/plane/SKILL.md`, `inject`, generated `skills`) are updated with one consistent rule.

**Tech Stack:** Go 1.x, `spf13/cobra`, `stretchr/testify`, `net/http/httptest`, `yaml.v3`, existing `internal/api`, `internal/config`, `internal/output`.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-11-23-snapshot-bulk-collection-design.md`.
- Preserve server fields: records hold `json.RawMessage`; never unmarshal into `plane.Issue`.
- Cursor pagination: `per_page=100`, `expand=assignees,state,labels`, 10,000-page safety cap, missing/repeated cursor is an error.
- Errors: 401/403 fail immediately; 429 bounded backoff (max 3 retries, max 30s delay); no partial snapshot or summary on failure.
- `--workspace` and `--project` are **persistent root flags**; do not redefine them. Use global `--workspace`/`--project` and a new repeatable `--only-project`.
- `--all-workspaces` is mutually exclusive with a selected workspace: an explicit global `--workspace` **or** a configured workspace (`config.Cfg.DefaultWorkspace` / `PLANE_WORKSPACE`).
- `snapshot` never inherits `config.Cfg.DefaultProject`: the configured default project must not silently narrow a workspace-scoped snapshot. The global `--project` is used as a single selector only when it was explicitly passed on the command line; omitting it means "every project in scope".
- Bulk records go only to the output file; stdout carries a small summary only.
- View hint: 3 `issue view` calls within 5 minutes per workspace/project; warn once per window; stderr only; cache errors are non-fatal; no disable flag.
- Standard Go formatting, ASCII source, `TestXxx` tests alongside code, short imperative commit subjects.

---

### Task 1: Snapshot types and local time filter

**Files:**
- Create: `internal/timeutil/timeutil.go`
- Create: `internal/timeutil/timeutil_test.go`
- Modify: `cmd/report/report.go` (remove local `parseTime`, use `timeutil.ParseTime`)
- Create: `internal/snapshot/types.go`
- Create: `internal/snapshot/timefilter.go`
- Create: `internal/snapshot/timefilter_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `timeutil.ParseTime(value string, now time.Time) (time.Time, error)`; `snapshot.Record`, `snapshot.ProjectCoverage`, `snapshot.Scope`, `snapshot.Snapshot`, `snapshot.Summary`, `snapshot.FilterRecords(records []Record, scope Scope, now time.Time) ([]Record, error)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/timeutil/timeutil_test.go`:

```go
package timeutil

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTime(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		input string
		want  string
	}{
		{"7d", "2026-11-16T12:00:00Z"},
		{"24h", "2026-11-22T12:00:00Z"},
		{"2026-11-01T00:00:00Z", "2026-11-01T00:00:00Z"},
		{"2026-11-01", "2026-11-01T00:00:00Z"},
	}
	for _, tt := range tests {
		got, err := ParseTime(tt.input, now)
		require.NoError(t, err, tt.input)
		assert.Equal(t, tt.want, got.UTC().Format(time.RFC3339), tt.input)
	}

	_, err := ParseTime("nonsense", now)
	assert.Error(t, err)
}
```

Create `internal/snapshot/timefilter_test.go` (without a duplicate parse test):

```go
package snapshot

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func recordWithTimes(t *testing.T, created, updated string) Record {
	t.Helper()
	item, err := json.Marshal(map[string]string{
		"id": "i-1", "created_at": created, "updated_at": updated,
	})
	require.NoError(t, err)
	return Record{Workspace: "ws", ProjectID: "p1", ProjectIdentifier: "GAME", Item: item}
}

func TestFilterRecordsWithoutRangeKeepsEverything(t *testing.T) {
	records := []Record{recordWithTimes(t, "", "")}
	got, err := FilterRecords(records, Scope{}, time.Now())
	require.NoError(t, err)
	assert.Equal(t, records, got)
}

func TestFilterRecordsUsesCreatedFieldInclusively(t *testing.T) {
	records := []Record{
		recordWithTimes(t, "2026-11-01T00:00:00Z", "2026-11-20T00:00:00Z"),
		recordWithTimes(t, "2026-10-31T23:59:59Z", "2026-11-21T00:00:00Z"),
		recordWithTimes(t, "2026-11-30T00:00:00Z", "2026-11-22T00:00:00Z"),
	}
	scope := Scope{DateField: "created", Since: "2026-11-01", Until: "2026-11-30"}

	got, err := FilterRecords(records, scope, time.Now())
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "2026-11-01T00:00:00Z", timeFromRecord(t, got[0], "created"))
}

func TestFilterRecordsUsesUpdatedFieldByDefault(t *testing.T) {
	records := []Record{
		recordWithTimes(t, "2026-01-01T00:00:00Z", "2026-11-10T00:00:00Z"),
		recordWithTimes(t, "2026-11-01T00:00:00Z", "2026-10-01T00:00:00Z"),
	}
	got, err := FilterRecords(records, Scope{Since: "2026-11-01"}, time.Now())
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestFilterRecordsSkipsItemsWithoutUsableTimestamp(t *testing.T) {
	item, err := json.Marshal(map[string]string{"id": "i-2"})
	require.NoError(t, err)
	records := []Record{{Workspace: "ws", ProjectID: "p1", Item: item}}

	got, err := FilterRecords(records, Scope{Since: "2026-11-01"}, time.Now())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestFilterRecordsRejectsInvalidInputs(t *testing.T) {
	_, err := FilterRecords(nil, Scope{DateField: "completed"}, time.Now())
	assert.ErrorContains(t, err, "date field")

	_, err = FilterRecords(nil, Scope{Since: "bad"}, time.Now())
	assert.ErrorContains(t, err, "since")
}

func timeFromRecord(t *testing.T, record Record, field string) string {
	t.Helper()
	var item map[string]string
	require.NoError(t, json.Unmarshal(record.Item, &item))
	return item[field+"_at"]
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/timeutil ./internal/snapshot -v`
Expected: build failure — `internal/timeutil` has no `ParseTime`, and package `snapshot` has no non-test files defining `FilterRecords`, `Scope`, or `Record`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/timeutil/timeutil.go`:

```go
package timeutil

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseTime accepts Nd days, a Go duration, RFC3339, or a date-only value in
// now.Location(). It is shared by report and snapshot so their windows cannot
// drift apart.
func ParseTime(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err == nil && days >= 0 {
			return now.AddDate(0, 0, -days), nil
		}
	}
	if duration, err := time.ParseDuration(value); err == nil {
		return now.Add(-duration), nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	if parsed, err := time.ParseInLocation("2006-01-02", value, now.Location()); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q", value)
}
```

Modify `cmd/report/report.go`: add the import `"github.com/ba0f3/plane-cli/internal/timeutil"`, delete the entire local `func parseTime(v string, now time.Time) (time.Time, error) { ... }`, and replace its two call sites inside `issueInWindow` with `timeutil.ParseTime(...)`:

```go
		since, err := timeutil.ParseTime(reportSince, time.Now())
```
```go
		until, err := timeutil.ParseTime(reportUntil, time.Now())
```

Create `internal/snapshot/types.go`:

```go
package snapshot

import "encoding/json"

// Record keeps the server's work-item representation verbatim so callers see
// fields that the presentation layer of `issue list` does not project.
type Record struct {
	Workspace         string          `json:"workspace"`
	ProjectID         string          `json:"project_id"`
	ProjectIdentifier string          `json:"project_identifier"`
	Item              json.RawMessage `json:"item"`
}

type ProjectCoverage struct {
	Workspace  string `json:"workspace"`
	ProjectID  string `json:"project_id"`
	Items      int    `json:"items"`
	Retained   int    `json:"retained"`
	Pages      int    `json:"pages"`
	Duplicates int    `json:"duplicates"`
}

type Scope struct {
	Workspace     string   `json:"workspace,omitempty"`
	AllWorkspaces bool     `json:"all_workspaces"`
	Projects      []string `json:"projects,omitempty"`
	Expand        string   `json:"expand"`
	DateField     string   `json:"date_field,omitempty"`
	Since         string   `json:"since,omitempty"`
	Until         string   `json:"until,omitempty"`
}

type Snapshot struct {
	StartedAt  string            `json:"started_at"`
	FinishedAt string            `json:"finished_at"`
	Scope      Scope             `json:"scope"`
	Coverage   []ProjectCoverage `json:"coverage"`
	Records    []Record          `json:"records"`
}

// Summary is the only snapshot information printed to stdout.
type Summary struct {
	File        string            `json:"file"`
	StartedAt   string            `json:"started_at"`
	FinishedAt  string            `json:"finished_at"`
	UniqueItems int               `json:"unique_items"`
	Coverage    []ProjectCoverage `json:"coverage"`
}
```

Create `internal/snapshot/timefilter.go`:

```go
package snapshot

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ba0f3/plane-cli/internal/timeutil"
)

func itemTime(item json.RawMessage, field string) (time.Time, bool) {
	var stamps struct {
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.Unmarshal(item, &stamps); err != nil {
		return time.Time{}, false
	}
	raw := stamps.UpdatedAt
	if field == "created" {
		raw = stamps.CreatedAt
	}
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// FilterRecords applies the optional local time window to raw records. It does
// not mutate the input slice's elements and returns records with usable
// timestamps only when a filter is active.
func FilterRecords(records []Record, scope Scope, now time.Time) ([]Record, error) {
	if scope.Since == "" && scope.Until == "" {
		return records, nil
	}

	field := strings.ToLower(strings.TrimSpace(scope.DateField))
	if field == "" {
		field = "updated"
	}
	if field != "created" && field != "updated" {
		return nil, fmt.Errorf("invalid date field %q: use created or updated", scope.DateField)
	}

	var since, until time.Time
	var err error
	if scope.Since != "" {
		since, err = timeutil.ParseTime(scope.Since, now)
		if err != nil {
			return nil, fmt.Errorf("invalid --since: %w", err)
		}
	}
	if scope.Until != "" {
		until, err = timeutil.ParseTime(scope.Until, now)
		if err != nil {
			return nil, fmt.Errorf("invalid --until: %w", err)
		}
	}

	kept := make([]Record, 0, len(records))
	for _, record := range records {
		t, ok := itemTime(record.Item, field)
		if !ok {
			continue
		}
		if !since.IsZero() && t.Before(since) {
			continue
		}
		if !until.IsZero() && t.After(until) {
			continue
		}
		kept = append(kept, record)
	}
	return kept, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/timeutil ./internal/snapshot ./cmd/report -v`
Expected: PASS — `TestParseTime`, all `TestFilterRecords*` tests, and existing report tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/timeutil/timeutil.go internal/timeutil/timeutil_test.go cmd/report/report.go internal/snapshot/types.go internal/snapshot/timefilter.go internal/snapshot/timefilter_test.go
git commit -m "feat: add shared time parser and snapshot types"
```

---

### Task 2: Cursor collector

**Files:**
- Create: `internal/snapshot/collector.go`
- Create: `internal/snapshot/collector_test.go`

**Interfaces:**
- Consumes: `snapshot.Scope`, `snapshot.Record`, `snapshot.ProjectCoverage`, `snapshot.Snapshot`, `snapshot.FilterRecords` (Task 1).
- Produces: `snapshot.Collect(scope Scope) (*Snapshot, error)`; test helpers `projectRef`, `resolveProjects(scope, client)`, `collectProject(client, ref, expand, byID, sleep)`, `fetchPage(client, ref, expand, cursor, sleep)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/snapshot/collector_test.go`:

```go
package snapshot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClient(t *testing.T, handler http.Handler) (*api.Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &api.Client{
		HTTPClient: server.Client(), BaseURL: server.URL,
		APIKey: "test-key", Workspace: "ws",
	}, server
}

func writeProjects(t *testing.T, w http.ResponseWriter, projects ...map[string]string) {
	t.Helper()
	results := make([]json.RawMessage, 0, len(projects))
	for _, project := range projects {
		encoded, err := json.Marshal(project)
		require.NoError(t, err)
		results = append(results, encoded)
	}
	require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
		"results": results, "next_page_results": false,
	}))
}

func TestResolveProjectsSingleWorkspace(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/workspaces/ws/projects/", r.URL.Path)
		writeProjects(t, w, map[string]string{"id": "p1", "identifier": "GAME", "name": "Game"})
	}))

	refs, err := resolveProjects(Scope{Workspace: "ws"}, client)
	require.NoError(t, err)
	assert.Equal(t, []projectRef{{Workspace: "ws", ID: "p1", Identifier: "GAME"}}, refs)
}

func TestResolveProjectsFiltersBySelector(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProjects(t, w,
			map[string]string{"id": "p1", "identifier": "GAME", "name": "Game"},
			map[string]string{"id": "p2", "identifier": "WEB", "name": "Web"},
		)
	}))

	refs, err := resolveProjects(Scope{Workspace: "ws", Projects: []string{"game"}}, client)
	require.NoError(t, err)
	assert.Equal(t, []projectRef{{Workspace: "ws", ID: "p1", Identifier: "GAME"}}, refs)
}

func TestResolveProjectsAllWorkspaces(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/workspaces/":
			require.NoError(t, json.NewEncoder(w).Encode([]map[string]string{
				{"slug": "ws-a", "name": "A"}, {"slug": "ws-b", "name": "B"},
			}))
		case "/api/v1/workspaces/ws-a/projects/":
			writeProjects(t, w, map[string]string{"id": "p1", "identifier": "GAME"})
		case "/api/v1/workspaces/ws-b/projects/":
			writeProjects(t, w, map[string]string{"id": "p2", "identifier": "WEB"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))

	refs, err := resolveProjects(Scope{AllWorkspaces: true}, client)
	require.NoError(t, err)
	assert.Equal(t, []projectRef{
		{Workspace: "ws-a", ID: "p1", Identifier: "GAME"},
		{Workspace: "ws-b", ID: "p2", Identifier: "WEB"},
	}, refs)
}

func TestResolveProjectsRejectsAllWorkspacesWithWorkspace(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no HTTP request expected")
	}))

	_, err := resolveProjects(Scope{Workspace: "ws", AllWorkspaces: true}, client)
	assert.ErrorContains(t, err, "--all-workspaces")
}

func pageItem(id string) string {
	return fmt.Sprintf(`{"id":%q,"created_at":"2026-11-01T00:00:00Z","updated_at":"2026-11-02T00:00:00Z"}`, id)
}

func TestCollectProjectWalksCursorAndDedupes(t *testing.T) {
	var requests int
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/workspaces/ws/projects/p1/work-items/", r.URL.Path)
		assert.Equal(t, "assignees,state,labels", r.URL.Query().Get("expand"))
		assert.Equal(t, "100", r.URL.Query().Get("per_page"))
		requests++
		switch r.URL.Query().Get("cursor") {
		case "":
			fmt.Fprintf(w, `{"results":[%s],"next_page_results":true,"next_cursor":"c1"}`, pageItem("a"))
		case "c1":
			fmt.Fprintf(w, `{"results":[%s,%s],"next_page_results":false}`, pageItem("a"), pageItem("b"))
		default:
			t.Fatal("unexpected cursor")
		}
	}))

	byID := map[string]Record{}
	cov, err := collectProject(client, projectRef{Workspace: "ws", ID: "p1", Identifier: "GAME"},
		"assignees,state,labels", byID, func(time.Duration) {})
	require.NoError(t, err)
	assert.Equal(t, 2, requests)
	assert.Equal(t, ProjectCoverage{
		Workspace: "ws", ProjectID: "p1", Items: 3, Retained: 2, Pages: 2, Duplicates: 1,
	}, cov)
	assert.Len(t, byID, 2)
}

func TestCollectProjectRejectsMalformedPagination(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"results":[%s]}`, pageItem("a"))
	}))

	_, err := collectProject(client, projectRef{Workspace: "ws", ID: "p1"}, "expand", map[string]Record{}, func(time.Duration) {})
	assert.ErrorContains(t, err, "next_page_results")
}

func TestCollectProjectRejectsRepeatedCursor(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"results":[%s],"next_page_results":true,"next_cursor":"c1"}`, pageItem("a"))
	}))

	_, err := collectProject(client, projectRef{Workspace: "ws", ID: "p1"}, "expand", map[string]Record{}, func(time.Duration) {})
	assert.ErrorContains(t, err, "repeated next cursor")
}

func TestFetchPageRetries429AndRespectsRetryAfter(t *testing.T) {
	var calls int
	var slept []time.Duration
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprintf(w, `{"results":[],"next_page_results":false}`)
	}))

	_, err := fetchPage(client, projectRef{Workspace: "ws", ID: "p1"}, "expand", "",
		func(d time.Duration) { slept = append(slept, d) })
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	assert.Equal(t, []time.Duration{2 * time.Second}, slept)
}

func TestFetchPageFailsOn401(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"denied"}`)
	}))

	_, err := fetchPage(client, projectRef{Workspace: "ws", ID: "p1"}, "expand", "", func(time.Duration) {})
	assert.ErrorContains(t, err, "token scope")
}

func TestCollectBuildsSnapshot(t *testing.T) {
	t.Setenv("PLANE_API_KEY", "test-key")
	originalHost, originalWorkspace := config.Cfg.APIHost, config.Cfg.DefaultWorkspace
	t.Cleanup(func() {
		config.Cfg.APIHost, config.Cfg.DefaultWorkspace = originalHost, originalWorkspace
	})

	client, server := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/workspaces/ws/projects/":
			writeProjects(t, w, map[string]string{"id": "p1", "identifier": "GAME"})
		case "/api/v1/workspaces/ws/projects/p1/work-items/":
			fmt.Fprintf(w, `{"results":[%s,%s],"next_page_results":false}`, pageItem("a"), pageItem("b"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	config.Cfg.APIHost, config.Cfg.DefaultWorkspace = server.URL, ""

	snapshot, err := Collect(Scope{Workspace: "ws", Expand: "assignees,state,labels"})
	require.NoError(t, err)
	assert.Len(t, snapshot.Records, 2)
	assert.Equal(t, 2, snapshot.Coverage[0].Items)
	assert.Equal(t, 2, snapshot.Coverage[0].Retained)
	assert.NotEmpty(t, snapshot.StartedAt)
	assert.NotEmpty(t, snapshot.FinishedAt)
	assert.NotNil(t, client)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/snapshot -run 'TestResolveProjects|TestCollect|TestFetchPage' -v`
Expected: build failure — `resolveProjects`, `projectRef`, `collectProject`, `fetchPage`, and `Collect` are undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/snapshot/collector.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/snapshot -v`
Expected: PASS — all Task 1 and Task 2 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/snapshot/collector.go internal/snapshot/collector_test.go
git commit -m "feat: add cursor-based snapshot collector"
```

---

### Task 3: `plane-cli snapshot` command

**Files:**
- Create: `cmd/snapshot/snapshot.go`
- Create: `cmd/snapshot/snapshot_test.go`
- Modify: `cmd/root.go` (import + `AddCommand`)

**Interfaces:**
- Consumes: `snapshot.Collect`, `snapshot.Scope`, `snapshot.Snapshot`, `snapshot.Summary` (Tasks 1-2).
- Produces: `snapshotcmd.SnapshotCmd`, `runSnapshot(cmd, args)`, `validate(cmd)`, `writeSnapshot(path, s)`, `printSummary(cmd, path, s)`, overridable `collect = snap.Collect` for tests.

**Flag table (scope decisions are deliberate; do not restore the config fallback):**

| Flag | Meaning |
| --- | --- |
| (global) `--workspace WS` | Workspace slug; overrides config. Do not redefine |
| `--all-workspaces` | Collect every visible workspace (IAT). Rejected when a workspace is selected by flag, env, or config |
| `--only-project SELECTOR` | Repeatable project filter: ID, identifier, or name |
| (global) `--project` | Single selector when `--only-project` is empty, honoured **only when explicitly passed**; the configured default project is never inherited |
| `--expand LIST` | Default `assignees,state,labels` |
| `--out PATH` | Default `./plane-snapshot.json` |
| `--since`, `--until`, `--date-field` | Local record filter; same parser as `report` |
| `-o, --output json\|yaml` | Format of the small stdout summary only |
| `--quiet` | Suppress stdout summary |

- [ ] **Step 1: Write the failing tests**

Create `cmd/snapshot/snapshot_test.go`:

```go
package snapshot

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ba0f3/plane-cli/internal/config"
	snap "github.com/ba0f3/plane-cli/internal/snapshot"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRejectsAllWorkspacesWithWorkspace(t *testing.T) {
	root := &cobra.Command{Use: "plane-cli"}
	root.PersistentFlags().String("workspace", "", "")
	sub := &cobra.Command{Use: "snapshot"}
	root.AddCommand(sub)
	require.NoError(t, root.PersistentFlags().Set("workspace", "ws"))

	allWorkspaces = true
	t.Cleanup(func() { allWorkspaces = false })

	err := validate(sub)
	assert.ErrorContains(t, err, "--all-workspaces")
}

func TestValidateRejectsInvalidDateField(t *testing.T) {
	dateField = "completed"
	t.Cleanup(func() { dateField = "updated" })

	err := validate(&cobra.Command{Use: "snapshot"})
	assert.ErrorContains(t, err, "date field")
}

func TestWriteSnapshotCreatesAtomicFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "snapshot.json")
	s := &snap.Snapshot{
		StartedAt: "2026-11-23T00:00:00Z", FinishedAt: "2026-11-23T00:01:00Z",
		Scope: snap.Scope{Workspace: "ws"}, Coverage: []snap.ProjectCoverage{}, Records: []snap.Record{},
	}

	require.NoError(t, writeSnapshot(path, s))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var decoded snap.Snapshot
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "ws", decoded.Scope.Workspace)
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestRunSnapshotWritesFileAndSummaryOnly(t *testing.T) {
	originalCollect, originalQuiet, originalOut := collect, quiet, outPath
	originalFormat, originalAll, originalProjects := config.Cfg.OutputFormat, allWorkspaces, onlyProjects
	t.Cleanup(func() {
		collect, quiet, outPath = originalCollect, originalQuiet, originalOut
		config.Cfg.OutputFormat, allWorkspaces, onlyProjects = originalFormat, originalAll, originalProjects
	})

	dir := t.TempDir()
	outPath = filepath.Join(dir, "snapshot.json")
	quiet = false
	config.Cfg.OutputFormat = "json"
	collect = func(scope snap.Scope) (*snap.Snapshot, error) {
		assert.Equal(t, "ws", scope.Workspace)
		assert.Equal(t, []string{"GAME"}, scope.Projects)
		return &snap.Snapshot{
			StartedAt: "start", FinishedAt: "end",
			Coverage: []snap.ProjectCoverage{{Workspace: "ws", ProjectID: "p1", Items: 1, Retained: 1, Pages: 1}},
			Records:  []snap.Record{{Workspace: "ws", ProjectID: "p1"}},
		}, nil
	}
	allWorkspaces, onlyProjects, dateField = false, []string{"GAME"}, "updated"

	var out bytes.Buffer
	cmd := &cobra.Command{Use: "snapshot"}
	cmd.SetOut(&out)

	require.NoError(t, runSnapshot(cmd, nil))

	assert.FileExists(t, outPath)
	assert.Contains(t, out.String(), `"unique_items": 1`)
	assert.Contains(t, out.String(), `"file"`)
	assert.NotContains(t, out.String(), `"records"`)
	assert.True(t, strings.HasPrefix(strings.TrimSpace(out.String()), "{"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/snapshot -v`
Expected: build failure — `validate`, `writeSnapshot`, `runSnapshot`, and the flag variables are undefined.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/snapshot/snapshot.go`:

```go
package snapshot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/ba0f3/plane-cli/internal/output"
	snap "github.com/ba0f3/plane-cli/internal/snapshot"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	allWorkspaces bool
	onlyProjects  []string
	expand        string
	outPath       string
	since         string
	until         string
	dateField     string
	quiet         bool
)

// collect is a variable so command tests can substitute a stub without an API.
var collect = snap.Collect

var SnapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Collect a complete work-item snapshot in one invocation",
	Long: `Collect every selected work item into one local JSON snapshot.

The command paginates the work-items endpoint once per project, preserves the
server's raw fields, writes the bulk data to --out, and prints only a small
coverage summary to stdout. Use one snapshot for a report and calculate locally
instead of calling issue view for each task.`,
	Args: cobra.NoArgs,
	RunE: runSnapshot,
}

func init() {
	SnapshotCmd.Flags().BoolVar(&allWorkspaces, "all-workspaces", false, "Collect every visible workspace (IAT)")
	SnapshotCmd.Flags().StringArrayVar(&onlyProjects, "only-project", nil, "Project ID, identifier, or name (repeatable)")
	SnapshotCmd.Flags().StringVar(&expand, "expand", "assignees,state,labels", "Comma-separated work-item expansions")
	SnapshotCmd.Flags().StringVar(&outPath, "out", "./plane-snapshot.json", "Path for the bulk snapshot JSON")
	SnapshotCmd.Flags().StringVar(&since, "since", "", "Start of local filter: RFC3339, YYYY-MM-DD, duration, or Nd")
	SnapshotCmd.Flags().StringVar(&until, "until", "", "End of local filter: RFC3339 or YYYY-MM-DD")
	SnapshotCmd.Flags().StringVar(&dateField, "date-field", "updated", "Field for the local filter: created or updated")
	SnapshotCmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress the stdout summary")
}

func validate(cmd *cobra.Command) error {
	if allWorkspaces && cmd.InheritedFlags().Changed("workspace") {
		return fmt.Errorf("--all-workspaces cannot be combined with --workspace")
	}
	field := strings.ToLower(strings.TrimSpace(dateField))
	if field != "" && field != "created" && field != "updated" {
		return fmt.Errorf("invalid date field %q: use created or updated", dateField)
	}
	return nil
}

func runSnapshot(cmd *cobra.Command, args []string) error {
	if err := validate(cmd); err != nil {
		return err
	}

	scope := snap.Scope{
		Expand: expand, Since: since, Until: until, DateField: dateField,
		Projects: onlyProjects, AllWorkspaces: allWorkspaces,
	}
	if allWorkspaces {
		scope.Workspace = ""
	} else {
		scope.Workspace = config.Cfg.DefaultWorkspace
	}
	if len(scope.Projects) == 0 && strings.TrimSpace(config.Cfg.DefaultProject) != "" {
		scope.Projects = []string{config.Cfg.DefaultProject}
	}

	result, err := collect(scope)
	if err != nil {
		return err
	}
	if err := writeSnapshot(outPath, result); err != nil {
		return err
	}
	if quiet {
		return nil
	}
	return printSummary(cmd, outPath, result)
}

func writeSnapshot(path string, snapshot *snap.Snapshot) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".plane-snapshot-*.json")
	if err != nil {
		return fmt.Errorf("create temporary snapshot: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close snapshot: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install snapshot: %w", err)
	}
	return nil
}

func printSummary(cmd *cobra.Command, path string, snapshot *snap.Snapshot) error {
	summary := snap.Summary{
		File: path, StartedAt: snapshot.StartedAt, FinishedAt: snapshot.FinishedAt,
		UniqueItems: len(snapshot.Records), Coverage: snapshot.Coverage,
	}

	var buf bytes.Buffer
	switch output.NormalizeFormat(config.Cfg.OutputFormat) {
	case "json":
		encoder := json.NewEncoder(&buf)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(summary); err != nil {
			return err
		}
	case "yaml":
		encoder := yaml.NewEncoder(&buf)
		if err := encoder.Encode(summary); err != nil {
			return err
		}
		if err := encoder.Close(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported summary format %q", config.Cfg.OutputFormat)
	}

	_, err := cmd.OutOrStdout().Write(buf.Bytes())
	return err
}
```

Modify `cmd/root.go`: add the import after the `report` import:

```go
	snapshotcmd "github.com/ba0f3/plane-cli/cmd/snapshot"
```

and add after `rootCmd.AddCommand(report.ReportCmd)`:

```go
	rootCmd.AddCommand(snapshotcmd.SnapshotCmd)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/snapshot ./... -run 'TestValidate|TestWriteSnapshot|TestRunSnapshot|TestRenderSkill' -v`
Expected: PASS — new command tests and existing skill tests pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/snapshot/snapshot.go cmd/snapshot/snapshot_test.go cmd/root.go
git commit -m "feat: add plane-cli snapshot command"
```

---

### Task 4: Point agent instructions at the snapshot workflow

**Files:**
- Modify: `skills/plane/SKILL.md`
- Modify: `cmd/inject/inject.go`
- Modify: `cmd/inject/inject_test.go`
- Modify: `cmd/skills.go`
- Modify: `cmd/skills_test.go`

**Interfaces:**
- Consumes: `plane-cli snapshot` command from Task 3.
- Produces: identical rule text on all three surfaces; tests assert its presence.

- [ ] **Step 1: Write the failing tests**

In `cmd/inject/inject_test.go`, append:

```go
func TestGenerateContentPointsToSnapshot(t *testing.T) {
	resetModuleFlags()
	t.Cleanup(resetModuleFlags)

	content := generateContent()

	assert.Contains(t, content, "### Reporting and bulk analysis")
	assert.Contains(t, content, "plane-cli snapshot --workspace <slug> --out ./plane-snapshot.json -o json")
	assert.Contains(t, content, "Do not run `issue list` followed by `issue view <id>` for every task")
}
```

In `cmd/skills_test.go`, replace `TestRenderSkillDescribesRunningCLI` with:

```go
func TestRenderSkillDescribesRunningCLI(t *testing.T) {
	skill := renderSkill(rootCmd)

	assert.Contains(t, skill, "---\nname: plane-cli\n")
	assert.Contains(t, skill, "# Plane CLI Agent Skill")
	assert.Contains(t, skill, "PLANE_API_HOST")
	assert.Contains(t, skill, "### `plane-cli auth context`")
	assert.Contains(t, skill, "### `plane-cli workspace list`")
	assert.Contains(t, skill, "### `plane-cli project list`")
	assert.Contains(t, skill, "### `plane-cli report summary`")
	assert.Contains(t, skill, "### `plane-cli digest`")
	assert.Contains(t, skill, "### `plane-cli admin`")
	assert.Contains(t, skill, "### `plane-cli wiki create`")
	assert.Contains(t, skill, "`--file`: Read Markdown page content from a UTF-8 file")
	assert.Contains(t, skill, "### `plane-cli raw`")
	assert.Contains(t, skill, "### `plane-cli skills`")
	assert.Contains(t, skill, "`-o, --output`: Output format: json, yaml")
	assert.Contains(t, skill, "### `plane-cli snapshot`")
	assert.Contains(t, skill, "collect one snapshot and calculate locally")
	assert.Contains(t, skill, "Do not run `issue list` followed by `issue view <id>` for every task")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/inject ./cmd -run 'TestGenerateContentPointsToSnapshot|TestRenderSkillDescribesRunningCLI' -v`
Expected: FAIL — the generated surfaces do not yet contain the snapshot rule.

- [ ] **Step 3: Update `cmd/inject/inject.go`**

In `generateContent()`, immediately before the `// Keep the original sections for reference and tips` comment (which precedes ``content += `### Full Command Reference``), insert:

```go
	// Bulk reporting rule: override the per-item issue workflow for analysis requests.
	content += `### Reporting and bulk analysis

For workload, report, or analysis requests, collect one snapshot and calculate locally:

` + "```" + `bash
plane-cli snapshot --workspace <slug> --out ./plane-snapshot.json -o json
` + "```" + `

Do not run ` + "`issue list`" + ` followed by ` + "`issue view <id>`" + ` for every task. ` + "`snapshot`" + ` paginates the full work-item collection once, preserves raw fields, and writes bulk data to a file; stdout carries only a small coverage summary. Use ` + "`--all-workspaces`" + ` with an IAT, ` + "`--only-project`" + ` to narrow scope, and ` + "`--since`" + `/` + "`--until`" + `/` + "`--date-field`" + ` to reduce the local file when needed.

`
```

- [ ] **Step 4: Update `cmd/skills.go`**

In `renderSkill()`, replace this exact text (the end of the numbered rules):

```go
8. Use ` + "`plane-cli <command> --help`" + ` for detailed runtime help if a flag or argument is unclear. Re-run ` + "`plane-cli skills`" + ` after upgrading the binary.
```

with:

```go
8. Use ` + "`plane-cli <command> --help`" + ` for detailed runtime help if a flag or argument is unclear. Re-run ` + "`plane-cli skills`" + ` after upgrading the binary.
9. For workload, report, or analysis requests, collect one snapshot and calculate locally: run ` + "`plane-cli snapshot`" + ` once and read its output file. Do not run ` + "`issue list`" + ` followed by ` + "`issue view <id>`" + ` for every task; that costs one CLI call and one agent turn per item and hits API rate limits.
```

Then replace this exact text in the "Common agent workflows" block:

```go
# Workspace/project reporting
plane-cli --workspace design report summary -o json
plane-cli --workspace design report workload -o json
```

with:

```go
# Workspace/project reporting - collect once, then calculate locally
plane-cli --workspace design snapshot --out ./design-snapshot.json -o json
plane-cli --workspace design report summary -o json
```

- [ ] **Step 5: Update `skills/plane/SKILL.md`**

Replace the last row of the table in section 2 with:

```markdown
| Team comparison, closed-period reporting, label/product mix, deeper employee analysis | `plane-cli snapshot --workspace "$WS" --out "$PLANE_ANALYSIS_DIR/snapshot.json" -o json` | One shared raw dataset for all people, metrics and follow-up questions |
```

Add immediately after the table (before "Current behavior to account for..."):

```markdown
Do not run `issue list` followed by `issue view` for every task. That pattern
costs one CLI invocation and one agent turn per item and hits API rate limits.
```

Replace the whole fenced Python collector block in section 3 (from `python3 - "$PLANE_ANALYSIS_DIR" <<'PY'` through the closing `PY`) with:

```bash
plane-cli snapshot --workspace "$WS" --out "$PLANE_ANALYSIS_DIR/snapshot.json" -o json
```

Replace the paragraph immediately after that block (the one beginning "If the server returns a bare array...") with:

```markdown
`snapshot` walks cursor pagination for every selected project, preserves the
server's work-item fields, deduplicates by item ID, and records `coverage` with
fetched/retained counts. Inspect one record before aggregating: state, assignee
and label values can be UUID strings or expanded objects. Use
`--all-workspaces` with an IAT, `--only-project` to narrow scope, and
`--since` / `--until` / `--date-field` to reduce the local file; the applied
filter is recorded in `scope`. A short page alone does not prove completeness —
check `coverage` and stop on API errors. Keep raw JSON local and use one
Python/jq aggregation pass to emit compact tables and a shortlist of evidence
IDs. Reuse this snapshot for alternate groupings and follow-up questions.
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./cmd/... -v`
Expected: PASS — instruction-surface tests and all command tests pass.

- [ ] **Step 7: Commit**

```bash
git add skills/plane/SKILL.md cmd/inject/inject.go cmd/inject/inject_test.go cmd/skills.go cmd/skills_test.go
git commit -m "docs: steer agents to one-shot snapshot collection"
```

---

### Task 5: Repetitive `issue view` hint

**Files:**
- Create: `internal/viewhint/viewhint.go`
- Create: `internal/viewhint/viewhint_test.go`
- Modify: `cmd/issue/issue.go` (import + hook in `runView`)

**Interfaces:**
- Consumes: `config.Cfg.DefaultWorkspace`, `config.Cfg.DefaultProject`.
- Produces: `viewhint.Check(workspace, project string, now time.Time) (bool, error)`, `viewhint.Warning` constant. `runView` calls it and writes to `cmd.ErrOrStderr()`.
- The cache file is bounded: once it holds `maxEvents` (500) lines it is rewritten atomically (temp file + rename in the same directory) with only the events still inside the current 5-minute window, keeping room for one more append so the file never exceeds 500 lines. `warn` events are always kept so the once-per-window suppression survives the rewrite. Pruning is best-effort and must never break `issue view`.
- Tests inject the cache directory through the exported `viewhint.CacheDir` variable and restore it afterwards, so the cache file can be redirected into a temporary directory.

- [ ] **Step 1: Write the failing test**

Create `internal/viewhint/viewhint_test.go`:

```go
package viewhint

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useTempCache(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	original := CacheDir
	CacheDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { CacheDir = original })
}

func TestCheckWarnsAtThresholdOncePerWindow(t *testing.T) {
	useTempCache(t)
	start := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	for i := 0; i < Threshold-1; i++ {
		warn, err := Check("ws", "p1", start.Add(time.Duration(i)*time.Minute))
		require.NoError(t, err)
		assert.False(t, warn)
	}
	warn, err := Check("ws", "p1", start.Add(2*time.Minute))
	require.NoError(t, err)
	assert.True(t, warn)

	warn, err = Check("ws", "p1", start.Add(3*time.Minute))
	require.NoError(t, err)
	assert.False(t, warn)

	warn, err = Check("ws", "p1", start.Add(6*time.Minute))
	require.NoError(t, err)
	assert.True(t, warn)
}

func TestCheckIsolatesKeys(t *testing.T) {
	useTempCache(t)
	start := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	for i := 0; i < Threshold; i++ {
		_, err := Check("ws", "p1", start.Add(time.Duration(i)*time.Minute))
		require.NoError(t, err)
	}
	warn, err := Check("ws", "p2", start.Add(3*time.Minute))
	require.NoError(t, err)
	assert.False(t, warn)
}

func TestCheckReportsCacheFailure(t *testing.T) {
	original := CacheDir
	CacheDir = func() (string, error) { return "", fmt.Errorf("no cache dir") }
	t.Cleanup(func() { CacheDir = original })

	warn, err := Check("ws", "p1", time.Now())
	assert.Error(t, err)
	assert.False(t, warn)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/viewhint -v`
Expected: build failure — `viewhint` package has no `Check`, `Threshold`, or `Warning`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/viewhint/viewhint.go`:

```go
package viewhint

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Threshold is the number of issue view calls within Window that triggers
	// the one-shot hint for a workspace/project key.
	Threshold = 3
	Window    = 5 * time.Minute
	Warning   = "plane-cli: repeated issue view calls detected (3 in 5m); use plane-cli snapshot to collect all items in one invocation instead of viewing each task."
)

var cacheDir = os.UserCacheDir

type event struct {
	Key   string `json:"key"`
	Event string `json:"event"`
	At    string `json:"at"`
}

func eventsPath() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "plane-cli", "view-hints.jsonl"), nil
}

// Check records a view for the key and reports whether the periodic hint should
// be emitted. It writes at most one warn event per Window per key.
func Check(workspace, project string, now time.Time) (bool, error) {
	key := strings.TrimSpace(workspace) + "/" + strings.TrimSpace(project)
	filePath, err := eventsPath()
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return false, err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	if err := appendEvent(filePath, event{Key: key, Event: "view", At: stamp}); err != nil {
		return false, err
	}

	events, err := readEvents(filePath)
	if err != nil {
		return false, err
	}
	cutoff := now.Add(-Window)
	views, warned := 0, false
	for _, item := range events {
		if item.Key != key {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, item.At)
		if err != nil || at.Before(cutoff) {
			continue
		}
		switch item.Event {
		case "view":
			views++
		case "warn":
			warned = true
		}
	}
	if views < Threshold || warned {
		return false, nil
	}
	if err := appendEvent(filePath, event{Key: key, Event: "warn", At: stamp}); err != nil {
		return false, err
	}
	return true, nil
}

func appendEvent(filePath string, item event) error {
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	_, err = fmt.Fprintln(file, string(data))
	return err
}

func readEvents(filePath string) ([]event, error) {
	file, err := os.Open(filePath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var events []event
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var item event
		if err := json.Unmarshal(scanner.Bytes(), &item); err == nil {
			events = append(events, item)
		}
	}
	return events, scanner.Err()
}
```

- [ ] **Step 4: Hook into `runView`**

In `cmd/issue/issue.go`, add imports `"time"` and `"github.com/ba0f3/plane-cli/internal/viewhint"`. Replace the start of `runView`:

```go
func runView(cmd *cobra.Command, args []string) error {
	projectID := config.Cfg.DefaultProject
	if projectID == "" {
		return fmt.Errorf("no project specified")
	}

	issueID := args[0]
```

with the following, where the hint check runs **after** `resolveIssue` succeeded and before the formatter prints:

```go
func runView(cmd *cobra.Command, args []string) error {
	projectID := config.Cfg.DefaultProject
	if projectID == "" {
		return fmt.Errorf("no project specified")
	}

	issueID := args[0]

	client, err := api.NewClient()
	if err != nil {
		return err
	}

	issue, err := resolveIssue(client, projectID, issueID)
	if err != nil {
		return err
	}

	// Cache failures are ignored: the hint must never break issue view. Only
	// successful views count towards the hint threshold.
	if warn, err := viewhint.Check(config.Cfg.DefaultWorkspace, projectID, time.Now()); err == nil && warn {
		fmt.Fprintln(cmd.ErrOrStderr(), viewhint.Warning)
	}

	formatter := output.NewFormatter(config.Cfg.OutputFormat, false)
	return formatter.Print(issue)
}
```

Cache errors are intentionally ignored: the hint must never break `issue view`. The warning goes to stderr, so stdout JSON stays clean. Because the check runs only after the issue was resolved, a failed view never records a hint event and never counts towards the threshold.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/viewhint ./cmd/issue -v`
Expected: PASS — threshold, once-per-window, key isolation, and cache-failure tests pass; existing issue tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/viewhint/viewhint.go internal/viewhint/viewhint_test.go cmd/issue/issue.go
git commit -m "feat: hint at snapshot after repeated issue views"
```

---

### Task 6: Full verification and skill package

**Files:** no source changes; verification only.

**Interfaces:**
- Consumes: all previous tasks.

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: PASS — no failures, no skipped packages due to build errors.

- [ ] **Step 2: Run gofmt and vet**

Run: `gofmt -l .` then `go vet ./...`
Expected: `gofmt -l` prints nothing; `go vet` exits 0.

- [ ] **Step 3: Run the repository check target**

Run: `make check`
Expected: `fmt`, `vet`, `lint`, and `test` all succeed. If `golangci-lint` is not installed locally, report that explicitly and rely on steps 1-2 plus `make test`.

- [ ] **Step 4: Verify the packaged skill still builds and contains the rule**

Run:

```bash
make skill-zip
unzip -p dist/plane-skill.zip plane/SKILL.md | grep -F 'plane-cli snapshot --workspace "$WS" --out "$PLANE_ANALYSIS_DIR/snapshot.json" -o json'
unzip -p dist/plane-skill.zip plane/SKILL.md | grep -F 'Do not run `issue list` followed by `issue view`'
```

Expected: both `grep` commands print the matching line; no missing-file errors.

- [ ] **Step 5: Smoke-check the generated skill from the built binary**

Run:

```bash
make build
./plane-cli skills | grep -F '### `plane-cli snapshot`'
./plane-cli snapshot --help | head -30
```

Expected: the generated skill contains the snapshot command section; `--help` lists `--all-workspaces`, `--only-project`, `--out`, `--since`, `--until`, `--date-field`, and `--quiet`.

- [ ] **Step 6: Final status**

Run: `git status --short`
Expected: clean working tree (the `dist/` and `plane-cli` build artifacts are ignored or already tracked per repo policy; do not commit them).

---

## Self-review notes

- Spec coverage: problem/goal map to Tasks 1-4; reliability and schema validation to Task 2; file-only bulk output and small summary to Task 3; three instruction surfaces to Task 4; view hint to Task 5; verification to Task 6.
- Flag constraint discovered during planning is recorded in the spec amendment (commit `876af2a`) and in Tasks 2-3.
- The `parseTime` duplication the task-review rubric would flag is removed by extracting `internal/timeutil` and migrating `cmd/report` in Task 1.
- No placeholders: every code step includes complete source and exact run commands.
