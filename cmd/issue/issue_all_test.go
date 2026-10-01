package issue

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

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
	config.Cfg.OutputFormat = "json"
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

func mustMarshalIssues(t *testing.T, v interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

func issuePage(start, total, pageSize int) []plane.Issue {
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

func writeIssuePage(t *testing.T, w http.ResponseWriter, items []plane.Issue, total int) {
	t.Helper()
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(api.Response{
		Results:    mustMarshalIssues(t, items),
		Pagination: api.Pagination{Count: len(items), TotalResults: total},
	})
	require.NoError(t, err)
}

// queryOffset reads the offset query param, treating an absent first page as 0.
func queryOffset(t *testing.T, r *http.Request) int {
	t.Helper()
	raw := r.URL.Query().Get("offset")
	if raw == "" {
		return 0
	}
	offset, err := strconv.Atoi(raw)
	require.NoError(t, err)
	return offset
}

// setListFlags sets the package-level list flags and restores them after the test.
func setListFlags(t *testing.T, all bool, limit int) {
	t.Helper()
	prevAll, prevPerPage := listAll, perPage
	listAll, perPage = all, limit
	t.Cleanup(func() { listAll, perPage = prevAll, prevPerPage })
}

func TestRunListAllPaginates(t *testing.T) {
	calls := 0
	configureIssueCommandEnv(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeIssuePage(t, w, issuePage(queryOffset(t, r), 250, 100), 250)
	})
	setListFlags(t, true, 20)

	require.NoError(t, runList(nil, nil))

	assert.Equal(t, 3, calls)
}

func TestRunListWithoutAllSingleRequest(t *testing.T) {
	calls := 0
	configureIssueCommandEnv(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "20", r.URL.Query().Get("limit"))
		writeIssuePage(t, w, issuePage(queryOffset(t, r), 5, 20), 5)
	})
	setListFlags(t, false, 20)

	require.NoError(t, runList(nil, nil))

	assert.Equal(t, 1, calls)
}

func TestRunListAllForwardsFilters(t *testing.T) {
	configureIssueCommandEnv(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "state-1", r.URL.Query().Get("state"))
		assert.Equal(t, "user-1", r.URL.Query().Get("assignee"))
		writeIssuePage(t, w, issuePage(queryOffset(t, r), 5, 100), 5)
	})
	setListFlags(t, true, 20)

	prevState, prevAssignee := stateFilter, assigneeFilter
	stateFilter, assigneeFilter = "state-1", "user-1"
	t.Cleanup(func() { stateFilter, assigneeFilter = prevState, prevAssignee })

	require.NoError(t, runList(nil, nil))
}

func TestRunListAllAboveCeilingErrors(t *testing.T) {
	total := api.MaxAllIssues + 1
	configureIssueCommandEnv(t, func(w http.ResponseWriter, r *http.Request) {
		writeIssuePage(t, w, issuePage(queryOffset(t, r), total, 100), total)
	})
	setListFlags(t, true, 20)

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
	setListFlags(t, true, 20)

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
	setListFlags(t, false, 20)

	require.NoError(t, runSearch(nil, []string{"needle"}))
}
