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

// pagedResults builds a page of items starting at start, advancing by pageSize.
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
		writePage(t, w, pagedResults(queryOffset(t, r), total, 100), total)
	})
	defer closeServer()

	issues, err := client.ListAllIssuesPaged("proj-1", IssueListOptions{}, MaxAllIssues)

	require.NoError(t, err)
	assert.Len(t, issues, MaxAllIssues)
}

func TestListAllIssuesPagedErrorsAboveMax(t *testing.T) {
	total := MaxAllIssues + 1
	client, closeServer := newPagedTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writePage(t, w, pagedResults(queryOffset(t, r), total, 100), total)
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
