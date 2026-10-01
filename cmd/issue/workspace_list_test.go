package issue

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/pkg/plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceIssueMatchesFilters(t *testing.T) {
	issue := plane.Issue{
		State: plane.FlexibleState{ID: "state-1"},
		Assignees: []plane.FlexibleUser{
			{ID: "user-1", Email: "alice@example.com"},
		},
	}

	assert.True(t, workspaceIssueMatches(issue, "", ""))
	assert.True(t, workspaceIssueMatches(issue, "state-1", ""))
	assert.True(t, workspaceIssueMatches(issue, "STATE-1", "user-1"))
	assert.False(t, workspaceIssueMatches(issue, "state-2", ""))
	assert.False(t, workspaceIssueMatches(issue, "state-1", "user-2"))
}

func TestSortWorkspaceIssuesNewestFirst(t *testing.T) {
	older := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	newer := older.Add(2 * time.Hour)
	records := []workspaceIssueRecord{
		{Project: plane.Project{Identifier: "B"}, Issue: plane.Issue{SequenceID: 2, UpdatedAt: older}},
		{Project: plane.Project{Identifier: "A"}, Issue: plane.Issue{SequenceID: 3, UpdatedAt: newer}},
		{Project: plane.Project{Identifier: "A"}, Issue: plane.Issue{SequenceID: 1, UpdatedAt: older}},
	}

	sortWorkspaceIssues(records)

	require.Len(t, records, 3)
	assert.Equal(t, 3, records[0].Issue.SequenceID)
	assert.Equal(t, "A", records[1].Project.Identifier)
	assert.Equal(t, 1, records[1].Issue.SequenceID)
	assert.Equal(t, "B", records[2].Project.Identifier)
}

func TestIssueKeyUsesProjectIdentifier(t *testing.T) {
	project := plane.Project{ID: "project-id", Identifier: "WEB"}
	issue := plane.Issue{ID: "issue-id", SequenceID: 42}
	assert.Equal(t, "WEB-42", issueKey(project, issue))

	project.Identifier = ""
	assert.Equal(t, "issue-id", issueKey(project, issue))
}

func TestWorkspaceAssigneeNamesUsesBestAvailableIdentity(t *testing.T) {
	issue := plane.Issue{Assignees: []plane.FlexibleUser{
		{ID: "u1", DisplayName: "Alice"},
		{ID: "u2", FirstName: "Bob", LastName: "Nguyen"},
		{ID: "u3", Email: "carol@example.com"},
		{ID: "u4"},
	}}

	assert.Equal(t, []string{"Alice", "Bob Nguyen", "carol@example.com", "u4"}, workspaceAssigneeNames(issue))
}

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
		writeIssuePage(t, w, fullPage(queryOffset(t, r)), 1000)
	})
	defer closeServer()

	_, err := collectWorkspaceIssues(client, "", "", true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--limit")
}

func TestCollectWorkspaceIssuesWithoutAllIsUnbounded(t *testing.T) {
	client, closeServer := newWorkspaceTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/projects/") {
			w.WriteHeader(http.StatusOK)
			require.NoError(t, json.NewEncoder(w).Encode(api.Response{
				Results: mustMarshalIssues(t, []plane.Project{{ID: "proj-1", Identifier: "A"}}),
			}))
			return
		}
		// Cursor pagination: no offset param, one page of two items.
		assert.Empty(t, r.URL.Query().Get("offset"))
		w.WriteHeader(http.StatusOK)
		require.NoError(t, json.NewEncoder(w).Encode(api.Response{
			Results: mustMarshalIssues(t, []plane.Issue{{ID: "issue-1"}, {ID: "issue-2"}}),
		}))
	})
	defer closeServer()

	records, err := collectWorkspaceIssues(client, "", "", false)

	require.NoError(t, err)
	assert.Len(t, records, 2)
}

func TestCollectWorkspaceIssuesDescendsBudgetAcrossProjects(t *testing.T) {
	// proj-1 answers with a short page so it ends after one request; proj-2
	// keeps answering with full pages so it can exhaust whatever budget it is given.
	page := func(project string, start, count int) []plane.Issue {
		out := make([]plane.Issue, 0, count)
		for i := start; i < start+count; i++ {
			out = append(out, plane.Issue{ID: fmt.Sprintf("%s-%d", project, i)})
		}
		return out
	}

	requests := map[string]int{}
	client, closeServer := newWorkspaceTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/projects/") {
			w.WriteHeader(http.StatusOK)
			require.NoError(t, json.NewEncoder(w).Encode(api.Response{
				Results: mustMarshalIssues(t, []plane.Project{{ID: "proj-1"}, {ID: "proj-2"}}),
			}))
			return
		}
		project := strings.TrimSuffix(r.URL.Path[strings.Index(r.URL.Path, "/projects/")+len("/projects/"):], "/work-items/")
		requests[project]++
		if project == "proj-1" {
			writeIssuePage(t, w, page(project, 0, 60), 60)
			return
		}
		writeIssuePage(t, w, page(project, queryOffset(t, r), 100), 5000)
	})
	defer closeServer()

	_, err := collectWorkspaceIssues(client, "", "", true)

	// proj-1 returns 60 issues in one request, leaving proj-2 a 440 budget. proj-2
	// answers with full pages, so it burns that budget in five requests instead
	// of the six a full 500 ceiling would take, which is how the descending
	// budget is observable.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.Equal(t, 1, requests["proj-1"])
	assert.Equal(t, 5, requests["proj-2"])
}
