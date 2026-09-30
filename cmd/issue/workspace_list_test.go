package issue

import (
	"testing"
	"time"

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
