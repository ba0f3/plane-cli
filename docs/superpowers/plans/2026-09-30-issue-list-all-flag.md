# Issue Listing `--all` Flag Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `--all` boolean flag to `issue list`, `issue search`, and `issue list --all-projects` that disables result truncation and fetches the complete result set, bounded by a hard limit of 500 issues per invocation.

**Architecture:** `internal/api` gains one pagination function plus one exported constant, using offset pagination because that is the only pagination style proven to compose with the `state`/`assignee` query filters on this endpoint. `cmd/issue` owns flag parsing and translates the ceiling into a user-facing error. Output columns and JSON shape are untouched.

**Tech Stack:** Go, Cobra, testify (`assert`/`require`), `net/http/httptest`.

## Global Constraints

- Hard ceiling is exactly **500** issues per invocation, exported as `api.MaxAllIssues`.
- Exceeding the ceiling is an **error**, never a silent truncation.
- `internal/api.ListAllIssues` must keep its current unbounded cursor-walking behaviour; `cmd/report` and `cmd/digest` depend on it. Do not modify it.
- Output columns for `issue list` stay exactly `ID, #, TITLE, STATE, PRIORITY, ASSIGNEE`. JSON keys stay exactly `id, sequence_id, title, state, priority, assignee`.
- Page size for `--all` is a fixed `100`. It is not configurable via `--limit`.
- `--all` and `--limit` together are not an error; `--all` wins and `--limit` is ignored.
- Offset pagination, not cursor. Cursor + `state`/`assignee` filters are unverified against this endpoint and could silently return wrong data.
- Run `gofmt` before every commit.
- Run `make check` (fmt, vet, lint, test) before the final commit of Task 4.

---

### Task 1: Paginated fetch in the API layer

**Files:**
- Modify: `internal/api/issues.go` (append after `ListIssues`, which ends at line 48)
- Create: `internal/api/issues_paged_test.go`

**Interfaces:**
- Consumes: existing `api.IssueListOptions` (fields `State string`, `Assignee string`), existing `api.Response` / `api.Pagination`, existing `(*Client).Get(path, query, v)`.
- Produces:
  - `const MaxAllIssues = 500`
  - `func (c *Client) ListAllIssuesPaged(projectID string, opts IssueListOptions, max int) ([]plane.Issue, error)`

  Task 2 and Task 3 call `ListAllIssuesPaged` and reference `MaxAllIssues`.

- [ ] **Step 1: Write the failing test**

Create `internal/api/issues_paged_test.go`:

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/ba0f3/plane-cli/pkg/plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPagedTestClient(t *testing.T, handler http.HandlerFunc) (*Client, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := &Client{
		HTTPClient: &http.Client{Timeout: DefaultTimeout},
		BaseURL:    server.URL,
		APIKey:     "test-api-key",
		Workspace:  "test-workspace",
	}
	return client, server.Close
}

// pagedResults builds a page of total items starting at start, advancing by pageSize.
func pagedResults(start, total, pageSize int) []plane.Issue {
	end := start + pageSize
	if end > total {
		end = total
	}
	out := make([]plane.Issue, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, plane.Issue{
			ID:         fmt.Sprintf("issue-%d", i),
			SequenceID: i + 1,
			Name:       fmt.Sprintf("Issue %d", i),
		})
	}
	return out
}

func writePage(t *testing.T, w http.ResponseWriter, items []plane.Issue, total int) {
	t.Helper()
	raw, err := json.Marshal(items)
	require.NoError(t, err)
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(Response{
		Results:    raw,
		Pagination: Pagination{Count: len(items), TotalResults: total, TotalPages: 1},
	})
	require.NoError(t, err)
}

func TestListAllIssuesPagedSingleShortPage(t *testing.T) {
	calls := 0
	client, closeServer := newPagedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "/api/v1/workspaces/test-workspace/projects/proj-1/work-items/", r.URL.Path)
		assert.Equal(t, "100", r.URL.Query().Get("limit"))
		assert.Empty(t, r.URL.Query().Get("offset"))
		writePage(t, w, pagedResults(0, 3, 100), 3)
	})
	defer closeServer()

	issues, err := client.ListAllIssuesPaged("proj-1", IssueListOptions{}, MaxAllIssues)

	require.NoError(t, err)
	assert.Len(t, issues, 3)
	assert.Equal(t, 1, calls)
}

func TestListAllIssuesPagedStopsAtMax(t *testing.T) {
	total := 500
	client, closeServer := newPagedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		require.NoError(t, err)
		writePage(t, w, pagedResults(offset, total, 100), total)
	})
	defer closeServer()

	issues, err := client.ListAllIssuesPaged("proj-1", IssueListOptions{}, MaxAllIssues)

	require.NoError(t, err)
	assert.Len(t, issues, MaxAllIssues)
}

func TestListAllIssuesPagedErrorsAboveMax(t *testing.T) {
	total := MaxAllIssues + 1
	client, closeServer := newPagedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		require.NoError(t, err)
		writePage(t, w, pagedResults(offset, total, 100), total)
	})
	defer closeServer()

	issues, err := client.ListAllIssuesPaged("proj-1", IssueListOptions{}, MaxAllIssues)

	require.Error(t, err)
	assert.Nil(t, issues)
	assert.Contains(t, err.Error(), "500")
	assert.Contains(t, err.Error(), "--limit")
}

func TestListAllIssuesPagedForwardsFiltersOnEveryPage(t *testing.T) {
	offsets := []string{}
	client, closeServer := newPagedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "state-1", r.URL.Query().Get("state"))
		assert.Equal(t, "user-1", r.URL.Query().Get("assignee"))
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		if offset == "100" {
			writePage(t, w, pagedResults(100, 150, 100), 150)
			return
		}
		writePage(t, w, pagedResults(0, 150, 100), 150)
	})
	defer closeServer()

	issues, err := client.ListAllIssuesPaged("proj-1",
		IssueListOptions{State: "state-1", Assignee: "user-1"}, MaxAllIssues)

	require.NoError(t, err)
	assert.Len(t, issues, 150)
	assert.Equal(t, []string{"", "100"}, offsets)
}

func TestListAllIssuesPagedIgnoresLimitOption(t *testing.T) {
	client, closeServer := newPagedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "100", r.URL.Query().Get("limit"))
		writePage(t, w, pagedResults(0, 1, 100), 1)
	})
	defer closeServer()

	_, err := client.ListAllIssuesPaged("proj-1", IssueListOptions{Limit: 7}, MaxAllIssues)

	require.NoError(t, err)
}

func TestListAllIssuesPagedStopsOnEmptyPage(t *testing.T) {
	calls := 0
	client, closeServer := newPagedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		writePage(t, w, nil, 0)
	})
	defer closeServer()

	issues, err := client.ListAllIssuesPaged("proj-1", IssueListOptions{}, MaxAllIssues)

	require.NoError(t, err)
	assert.Empty(t, issues)
	assert.Equal(t, 1, calls)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api -run TestListAllIssuesPaged`

Expected: FAIL to compile — `undefined: MaxAllIssues`, `undefined: client.ListAllIssuesPaged`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/api/issues.go`:

```go
// MaxAllIssues is the hard ceiling applied when a caller opts out of result
// truncation. Exceeding it is reported as an error rather than silently
// returning a partial list.
const MaxAllIssues = 500

// allIssuesPageSize is the fixed page size used when walking the full result
// set. At the MaxAllIssues ceiling this costs five requests.
const allIssuesPageSize = 100

// ListAllIssuesPaged walks offset pagination for one project and returns every
// work item matching opts' State and Assignee filters. It returns an error,
// and no partial results, if the matching set is larger than max.
//
// Offset pagination is used deliberately: ListIssues already proves that
// limit/offset compose correctly with the state and assignee query
// parameters, whereas cursor pagination combined with those filters is
// unverified against this endpoint.
func (c *Client) ListAllIssuesPaged(projectID string, opts IssueListOptions, max int) ([]plane.Issue, error) {
	path := fmt.Sprintf("/workspaces/%s/projects/%s/work-items/", c.Workspace, projectID)

	if max <= 0 {
		max = MaxAllIssues
	}

	base := url.Values{}
	base.Set("expand", "assignees,state")
	if opts.State != "" {
		base.Set("state", opts.State)
	}
	if opts.Assignee != "" {
		base.Set("assignee", opts.Assignee)
	}
	base.Set("limit", strconv.Itoa(allIssuesPageSize))

	var all []plane.Issue
	for len(all) < max {
		query := url.Values{}
		for key, values := range base {
			for _, value := range values {
				query.Add(key, value)
			}
		}
		if len(all) > 0 {
			query.Set("offset", strconv.Itoa(len(all)))
		}

		var response Response
		if err := c.Get(path, query, &response); err != nil {
			return nil, err
		}

		var page []plane.Issue
		if len(response.Results) > 0 {
			if err := json.Unmarshal(response.Results, &page); err != nil {
				return nil, fmt.Errorf("decode work-item results: %w", err)
			}
		}
		all = append(all, page...)

		if len(page) < allIssuesPageSize {
			break
		}
	}

	if len(all) > max {
		return nil, fmt.Errorf("project has more than %d matching issues; use --limit to page through results", max)
	}

	return all, nil
}
```

Add `"strconv"` to the import block at the top of `internal/api/issues.go`. The block currently is:

```go
import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/ba0f3/plane-cli/pkg/plane"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `gofmt -w internal/api/issues.go internal/api/issues_paged_test.go && go test ./internal/api -run TestListAllIssuesPaged -v`

Expected: PASS, all six `TestListAllIssuesPaged*` tests ok.

- [ ] **Step 5: Verify no regression in the API package**

Run: `go test ./internal/api`

Expected: PASS, `ok github.com/ba0f3/plane-cli/internal/api`.

- [ ] **Step 6: Commit**

```bash
git add internal/api/issues.go internal/api/issues_paged_test.go
git commit -m "feat: add offset-paginated work-item fetch with result ceiling"
```

---

### Task 2: Wire `--all` into `issue list` and `issue search`

**Files:**
- Modify: `cmd/issue/issue.go:91-118` (register flag), `cmd/issue/issue.go:120-181` (`runList`), `cmd/issue/issue.go:382-422` (`runSearch`)
- Create: `cmd/issue/issue_all_test.go`

**Interfaces:**
- Consumes: `api.MaxAllIssues` and `api.Client.ListAllIssuesPaged` from Task 1; existing package vars `stateFilter`, `assigneeFilter`, `perPage`.
- Produces: package-level `var listAll bool`, plus package-level
  `func errTooManyIssues(max int) error`

  Task 3 reuses `listAll` in `runWorkspaceList` and `errTooManyIssues`.

- [ ] **Step 1: Write the failing test**

Create `cmd/issue/issue_all_test.go`:

```go
package issue

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/ba0f3/plane-cli/pkg/plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configureIssueCommandEnv(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	prevCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = prevCfg })

	t.Setenv("PLANE_API_KEY", "test-api-key")
	config.Cfg.APIHost = server.URL
	config.Cfg.DefaultWorkspace = "test-workspace"
	config.Cfg.DefaultProject = "proj-1"
	config.Cfg.OutputFormat = "table"
}

func issuePage(start, total, pageSize int) []plane.Issue {
	end := start + pageSize
	if end > total {
		end = total
	}
	out := make([]plane.Issue, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, plane.Issue{ID: fmt.Sprintf("issue-%d", i), SequenceID: i + 1, Name: fmt.Sprintf("Issue %d", i)})
	}
	return out
}

func writeIssuePage(t *testing.T, w http.ResponseWriter, items []plane.Issue, total int) {
	t.Helper()
	raw, err := json.Marshal(items)
	require.NoError(t, err)
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(api.Response{
		Results:    raw,
		Pagination: api.Pagination{Count: len(items), TotalResults: total},
	})
	require.NoError(t, err)
}

func TestRunListAllPaginates(t *testing.T) {
	calls := 0
	configureIssueCommandEnv(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		require.NoError(t, err)
		writeIssuePage(t, w, issuePage(offset, 250, 100), 250)
	})

	prevAll, prevPerPage := listAll, perPage
	listAll, perPage = true, 20
	t.Cleanup(func() { listAll, perPage = prevAll, prevPerPage })

	require.NoError(t, runList(nil, nil))

	assert.Equal(t, 3, calls)
}

func TestRunListWithoutAllSingleRequest(t *testing.T) {
	calls := 0
	configureIssueCommandEnv(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "20", r.URL.Query().Get("limit"))
		writeIssuePage(t, w, issuePage(0, 5, 20), 5)
	})

	prevAll, prevPerPage := listAll, perPage
	listAll, perPage = false, 20
	t.Cleanup(func() { listAll, perPage = prevAll, prevPerPage })

	require.NoError(t, runList(nil, nil))

	assert.Equal(t, 1, calls)
}

func TestRunListAllAboveCeilingErrors(t *testing.T) {
	configureIssueCommandEnv(t, func(w http.ResponseWriter, r *http.Request) {
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		require.NoError(t, err)
		writeIssuePage(t, w, issuePage(offset, api.MaxAllIssues+1, 100), api.MaxAllIssues+1)
	})

	prevAll, prevPerPage := listAll, perPage
	listAll, perPage = true, 20
	t.Cleanup(func() { listAll, perPage = prevAll, prevPerPage })

	err := runList(nil, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--limit")
}

func TestRunSearchAllAboveCeilingErrors(t *testing.T) {
	issues := make([]plane.Issue, 0, api.MaxAllIssues+1)
	for i := 0; i <= api.MaxAllIssues; i++ {
		issues = append(issues, plane.Issue{ID: fmt.Sprintf("issue-%d", i), SequenceID: i + 1})
	}

	configureIssueCommandEnv(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/workspaces/test-workspace/work-items/search/", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{"issues": issues}))
	})

	prevAll := listAll
	listAll = true
	t.Cleanup(func() { listAll = prevAll })

	err := runSearch(nil, []string{"needle"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--limit")
}

func TestRunSearchWithoutAllIgnoresCeiling(t *testing.T) {
	issues := make([]plane.Issue, 0, api.MaxAllIssues+1)
	for i := 0; i <= api.MaxAllIssues; i++ {
		issues = append(issues, plane.Issue{ID: fmt.Sprintf("issue-%d", i), SequenceID: i + 1})
	}

	configureIssueCommandEnv(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{"issues": issues}))
	})

	prevAll := listAll
	listAll = false
	t.Cleanup(func() { listAll = prevAll })

	require.NoError(t, runSearch(nil, []string{"needle"}))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/issue -run 'TestRunList|TestRunSearch'`

Expected: FAIL to compile — `undefined: listAll`.

- [ ] **Step 3: Add the flag variable and error helper**

In `cmd/issue/issue.go`, add near the other list flags (find the `var stateFilter` / `var perPage` declarations and add after them):

```go
// listAll disables result truncation when set. It supersedes perPage.
var listAll bool

// errTooManyIssues is the single error shape reported when a complete fetch
// exceeds the hard ceiling.
func errTooManyIssues(max int) error {
	return fmt.Errorf("more than %d matching issues; use --limit to page through results", max)
}
```

- [ ] **Step 4: Register the flags**

In `cmd/issue/issue.go` `init()`, after the existing line
`listCmd.Flags().IntVarP(&perPage, "limit", "l", 20, "Number of issues to show per page")`
add:

```go
	listCmd.Flags().BoolVar(&listAll, "all", false, "Fetch every matching issue instead of truncating to --limit (max 500)")
```

and after `IssueCmd.AddCommand(searchCmd)`'s sibling registrations — specifically alongside the other flag registrations — add:

```go
	searchCmd.Flags().BoolVar(&listAll, "all", false, "Fetch every matching issue instead of truncating (max 500)")
```

Also extend the `listCmd.Long` help text with one example line:

```
  plane-cli issue list --all --state <state-id>
```

- [ ] **Step 5: Branch in runList**

In `cmd/issue/issue.go` `runList`, replace the single fetch:

```go
	opts := api.IssueListOptions{
		State:    stateFilter,
		Assignee: assigneeFilter,
		Limit:    perPage,
	}

	issues, _, err := client.ListIssues(projectID, opts)
	if err != nil {
		return err
	}
```

with:

```go
	opts := api.IssueListOptions{
		State:    stateFilter,
		Assignee: assigneeFilter,
		Limit:    perPage,
	}

	var issues []plane.Issue
	if listAll {
		// --all supersedes --limit; perPage is deliberately not forwarded.
		paged, err := client.ListAllIssuesPaged(projectID,
			api.IssueListOptions{State: stateFilter, Assignee: assigneeFilter}, api.MaxAllIssues)
		if err != nil {
			return err
		}
		issues = paged
	} else {
		result, _, err := client.ListIssues(projectID, opts)
		if err != nil {
			return err
		}
		issues = result
	}
```

- [ ] **Step 6: Guard runSearch**

In `cmd/issue/issue.go` `runSearch`, insert immediately after the fetch succeeds, before the `len(issues) == 0` check:

```go
	if listAll && len(issues) > api.MaxAllIssues {
		return errTooManyIssues(api.MaxAllIssues)
	}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `gofmt -w cmd/issue/issue.go cmd/issue/issue_all_test.go && go test ./cmd/issue -run 'TestRunList|TestRunSearch' -v`

Expected: PASS, all six tests ok.

- [ ] **Step 8: Verify no regression**

Run: `go test ./cmd/issue ./internal/api`

Expected: PASS for both packages.

- [ ] **Step 9: Commit**

```bash
git add cmd/issue/issue.go cmd/issue/issue_all_test.go
git commit -m "feat: add --all flag to issue list and issue search"
```

---

### Task 3: Enforce the ceiling across a workspace's projects

**Files:**
- Modify: `cmd/issue/workspace_list.go:47-110`
- Modify: `cmd/issue/workspace_list_test.go` (append)

**Interfaces:**
- Consumes: `listAll` and `errTooManyIssues` from Task 2; `api.MaxAllIssues` and `api.Client.ListAllIssuesPaged` from Task 1.
- Produces: nothing new. `collectWorkspaceIssues` changes shape to
  `func collectWorkspaceIssues(client *api.Client, stateID, assigneeID string, all bool) ([]workspaceIssueRecord, error)`

  The existing `TestWorkspaceIssueMatchesFilters` and other tests in
  `cmd/issue/workspace_list_test.go` do not call `collectWorkspaceIssues`, so no
  existing test needs updating.

- [ ] **Step 1: Write the failing test**

Append to `cmd/issue/workspace_list_test.go`:

```go
func TestCollectWorkspaceIssuesAppliesCeilingAcrossProjects(t *testing.T) {
	fullPage := func(start int) []plane.Issue {
		out := make([]plane.Issue, 0, 100)
		for i := start; i < start+100; i++ {
			out = append(out, plane.Issue{ID: fmt.Sprintf("issue-%d", i), SequenceID: i + 1})
		}
		return out
	}

	client, closeServer := newWorkspaceTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/projects/") {
			w.WriteHeader(http.StatusOK)
			require.NoError(t, json.NewEncoder(w).Encode(api.Response{
				Results: mustMarshalIssues(t, []plane.Project{{ID: "proj-1", Identifier: "A"}, {ID: "proj-2", Identifier: "B"}}),
			}))
			return
		}
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		require.NoError(t, err)
		writeIssuePage(t, w, fullPage(offset), 1000)
	})
	defer closeServer()

	_, err := collectWorkspaceIssues(client, "", "", true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--limit")
}
```

Two helpers are needed. Append them to `cmd/issue/issue_all_test.go` (same
package, so they are visible to `workspace_list_test.go`):

```go
func mustMarshalIssues(t *testing.T, v interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

// newWorkspaceTestClient returns a bare api.Client pointed at a test server, for
// tests that call collectWorkspaceIssues directly and therefore bypass NewClient.
func newWorkspaceTestClient(t *testing.T, handler http.HandlerFunc) (*api.Client, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := &api.Client{
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		BaseURL:    server.URL,
		APIKey:     "test-api-key",
		Workspace:  "test-workspace",
	}
	return client, server.Close
}
```

Add the imports `"time"` to `cmd/issue/issue_all_test.go`.

Also add these imports to `cmd/issue/workspace_list_test.go`: `"encoding/json"`,
`"net/http"`, `"net/http/httptest"`, `"strconv"`, `"strings"`,
`"github.com/ba0f3/plane-cli/internal/api"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/issue -run TestCollectWorkspaceIssuesAppliesCeilingAcrossProjects`

Expected: FAIL to compile — `too many arguments in call to collectWorkspaceIssues`.

- [ ] **Step 3: Change collectWorkspaceIssues**

In `cmd/issue/workspace_list.go`, replace `collectWorkspaceIssues` with:

```go
// collectWorkspaceIssues gathers issues across every visible project. When all
// is false the fetch is unbounded and perPage truncation happens in the caller,
// matching the historical behaviour. When all is true each project is fetched
// with the ceiling budget remaining after the previous projects, so the ceiling
// applies to the workspace aggregate rather than per project.
func collectWorkspaceIssues(client *api.Client, stateID, assigneeID string, all bool) ([]workspaceIssueRecord, error) {
	projects, err := client.ListProjects()
	if err != nil {
		return nil, fmt.Errorf("list workspace projects: %w", err)
	}

	records := make([]workspaceIssueRecord, 0)
	for _, project := range projects {
		var issues []plane.Issue
		if all {
			budget := api.MaxAllIssues - len(records)
			issues, err = client.ListAllIssuesPaged(project.ID,
				api.IssueListOptions{State: stateID, Assignee: assigneeID}, budget)
		} else {
			issues, err = client.ListAllIssues(project.ID)
		}
		if err != nil {
			if all {
				return nil, errTooManyIssues(api.MaxAllIssues)
			}
			return nil, fmt.Errorf("project %s: list issues: %w", projectDisplay(project), err)
		}

		for _, issue := range issues {
			if !workspaceIssueMatches(issue, stateID, assigneeID) {
				continue
			}
			records = append(records, workspaceIssueRecord{Project: project, Issue: issue})
		}

		if all && len(records) >= api.MaxAllIssues {
			return nil, errTooManyIssues(api.MaxAllIssues)
		}
	}

	return records, nil
}
```

- [ ] **Step 4: Thread the flag through runWorkspaceList**

In `cmd/issue/workspace_list.go` `runWorkspaceList`, replace:

```go
	records, err := collectWorkspaceIssues(client, stateFilter, assigneeFilter)
	if err != nil {
		return err
	}

	sortWorkspaceIssues(records)
	if perPage > 0 && len(records) > perPage {
		records = records[:perPage]
	}
```

with:

```go
	records, err := collectWorkspaceIssues(client, stateFilter, assigneeFilter, listAll)
	if err != nil {
		return err
	}

	sortWorkspaceIssues(records)
	if !listAll && perPage > 0 && len(records) > perPage {
		records = records[:perPage]
	}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `gofmt -w cmd/issue/workspace_list.go cmd/issue/workspace_list_test.go cmd/issue/issue_all_test.go && go test ./cmd/issue -run TestCollectWorkspaceIssuesAppliesCeilingAcrossProjects -v`

Expected: PASS.

- [ ] **Step 6: Verify no regression**

Run: `go test ./cmd/issue`

Expected: PASS for the whole package, including the four pre-existing tests in `workspace_list_test.go`.

- [ ] **Step 7: Commit**

```bash
git add cmd/issue/workspace_list.go cmd/issue/workspace_list_test.go cmd/issue/issue_all_test.go
git commit -m "feat: enforce --all ceiling across workspace projects"
```

---

### Task 4: Document the flag and run the full gate

**Files:**
- Modify: `AGENTS.md` (the "Quick Start - Issue Management" fenced code block)
- Modify: `README.md` (search for the `plane-cli issue list` usage block)

**Interfaces:**
- Consumes: the `--all` flag registered in Task 2.
- Produces: no code interfaces.

**No integration test.** An earlier draft of this task created 501 real issues to
exercised the ceiling against a live Plane workspace. That was dropped: at the
5s pacing from `AGENTS.md` it is roughly 40 minutes of real writes, and a failure
partway through the loop leaves hundreds of orphaned issues behind. The six
`httptest` tests in Task 1 already cover the pagination loop, the filter
forwarding, and the ceiling behaviour, which is the logic actually at risk.

- [ ] **Step 1: Update the agent-facing docs**

In `AGENTS.md`, inside the `plane-cli issue list ...` fenced block, add ` [--all]` to the `plane-cli issue list` line so it reads:

```
plane-cli issue list [--output json] [--state <id:uuid>] [--assignee <id:uuid>]
                 [--limit <count:int>] [--all]
```

- [ ] **Step 2: Update the README**

Run `grep -n "issue list" README.md` to locate the usage block, then add
`[--all]` to the `plane-cli issue list` line in it, matching the line-break
style already used in that block.

- [ ] **Step 3: Confirm no integration-tagged code is affected**

Run: `go vet -tags integration ./...`

Expected: PASS with no vet errors. This task adds no code under the `integration`
build tag; the existing suite must still compile unchanged.

- [ ] **Step 4: Run the full gate**

Run: `make check`

Expected: `gofmt` clean, `go vet` clean, `golangci-lint` clean, all unit tests
pass.

- [ ] **Step 5: Verify the CLI surface by hand**

Run: `go run . issue list --help | grep -A2 all`

Expected: help text lists `[--all]` and the `max 500` description.

Run: `go run . issue search --help | grep all`

Expected: the same flag appears under search.

- [ ] **Step 6: Commit**

```bash
git add AGENTS.md README.md
git commit -m "docs: document --all issue listing flag"
```

---

## Self-Review

**Spec coverage.** Every spec section maps to a task:

- API-layer `MaxAllIssues` and `ListAllIssuesPaged` with offset pagination,
  fixed page size 100, and the `State`/`Assignee` filters: Task 1.
- Loop control (empty page, short page, reaching max) and error-instead-of-partial
  results: Task 1 Steps 3-4, covered by six tests.
- `--all` on `listCmd` and `searchCmd`; `--all` supersedes `--limit` without
  being an error: Task 2 Steps 4-5.
- `SearchIssues` unchanged, `--all` only enforces the ceiling: Task 2 Step 6.
- Workspace path drops `perPage` truncation under `--all` and applies the
  ceiling as an aggregate budget: Task 3 Steps 3-4.
- `ListAllIssues` untouched: no task modifies `internal/api/reporting.go`.
- Output columns and JSON keys unchanged: no task edits the `issueOutput` structs.
- Unit tests: Task 1 Step 1, Task 2 Step 1, Task 3 Step 1. No
  `integration`-tagged test is added, by design; Task 4 Step 3 exists only to
  confirm the tagged suite still compiles.

**Placeholder scan.** No TBD, no "add appropriate error handling", no "similar to
Task N". Every code step carries literal code. Task 2 Step 4 tells the implementer
to add the `searchCmd` flag registration "alongside the other flag registrations"
rather than repeating an identical block; that is a location hint, not a missing
decision, since the surrounding registrations are listed immediately above it.

**Type consistency.** `listAll` and `errTooManyIssues` are declared once in
`cmd/issue/issue.go` (Task 2 Step 3) and consumed in `workspace_list.go` (Task 3),
same package. `ListAllIssuesPaged(projectID string, opts IssueListOptions, max int)`
is called with that exact arity in Task 2 Step 5, Task 3 Step 3, and Task 4 Step 3.
`collectWorkspaceIssues` takes four parameters after Task 3; its only caller is
`runWorkspaceList`, updated in the same task.

**Naming.** Task 2's env-configuring helper is `configureIssueCommandEnv` and
Task 3's client-returning helper is `newWorkspaceTestClient`. They are distinct
names because the first only points config at a test server while the second
hands back an `*api.Client` for tests that bypass `NewClient`.
