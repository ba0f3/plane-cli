package digest

import (
	"testing"
	"time"

	"github.com/ba0f3/plane-cli/pkg/plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildWorkspaceDigest(t *testing.T) {
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	completedAt := now.Add(-2 * time.Hour)
	records := []Record{
		makeRecord("design", "DES", 1, "Overdue blocker", issueOptions{state: "In Progress", group: "started", priority: "high", targetDate: "2026-09-25", updatedAt: now.Add(-8 * 24 * time.Hour), labels: []string{"Blocked"}}),
		makeRecord("design", "DES", 2, "Due today", issueOptions{state: "Todo", group: "unstarted", targetDate: "2026-09-26", updatedAt: now.Add(-time.Hour)}),
		makeRecord("design", "DES", 3, "Completed", issueOptions{state: "Done", group: "completed", updatedAt: completedAt, completedAt: &completedAt}),
		makeRecord("engineering", "ENG", 4, "Other workspace", issueOptions{state: "Todo", group: "unstarted", updatedAt: now}),
	}

	report, err := Build(records, Options{Scope: "workspace", Selector: "design", Since: now.Add(-24 * time.Hour), Now: now, StaleDays: 7, Limit: 100})
	require.NoError(t, err)

	assert.Equal(t, 3, report.Summary.Matched)
	assert.Equal(t, 2, report.Summary.Active)
	assert.Equal(t, 1, report.Summary.Overdue)
	assert.Equal(t, 1, report.Summary.DueToday)
	assert.Equal(t, 1, report.Summary.Blocked)
	assert.Equal(t, 1, report.Summary.Stale)
	assert.Equal(t, 1, report.Summary.CompletedRecent)
	assert.Equal(t, 2, report.Summary.Unassigned)
	assert.Len(t, report.Sections.Overdue, 1)
	assert.Len(t, report.Sections.DueToday, 1)
	assert.Len(t, report.Sections.Blocked, 1)
	assert.Len(t, report.Sections.Stale, 1)
	assert.Len(t, report.Sections.Completed, 1)
	assert.Len(t, report.Sections.Unassigned, 2)
}

func TestBuildUserDigestMatchesEmail(t *testing.T) {
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	records := []Record{
		makeRecord("design", "DES", 1, "Alice task", issueOptions{state: "Todo", group: "unstarted", updatedAt: now, assignees: []plane.FlexibleUser{{ID: "u1", Email: "alice@example.com", DisplayName: "Alice"}}}),
		makeRecord("design", "DES", 2, "Bob task", issueOptions{state: "Todo", group: "unstarted", updatedAt: now, assignees: []plane.FlexibleUser{{ID: "u2", Email: "bob@example.com", DisplayName: "Bob"}}}),
	}

	report, err := Build(records, Options{Scope: "user", Selector: "ALICE@example.com", Since: now.Add(-24 * time.Hour), Now: now})
	require.NoError(t, err)
	assert.Equal(t, 1, report.Summary.Matched)
	assert.Equal(t, 1, report.Summary.Active)
	assert.Equal(t, 0, report.Summary.Unassigned)
}

func TestBuildProjectDigestMatchesIdentifier(t *testing.T) {
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	records := []Record{
		makeRecord("design", "DES", 1, "Design", issueOptions{state: "Todo", group: "unstarted", updatedAt: now}),
		makeRecord("design", "WEB", 2, "Web", issueOptions{state: "Todo", group: "unstarted", updatedAt: now}),
	}

	report, err := Build(records, Options{Scope: "project", Selector: "des", Since: now.Add(-24 * time.Hour), Now: now})
	require.NoError(t, err)
	assert.Equal(t, 1, report.Summary.Matched)
}

func TestBuildLimitTruncatesSectionsNotCounts(t *testing.T) {
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	records := []Record{
		makeRecord("design", "DES", 1, "A", issueOptions{state: "Todo", group: "unstarted", targetDate: "2026-09-20", updatedAt: now}),
		makeRecord("design", "DES", 2, "B", issueOptions{state: "Todo", group: "unstarted", targetDate: "2026-09-21", updatedAt: now}),
	}

	report, err := Build(records, Options{Scope: "workspace", Selector: "design", Since: now.Add(-24 * time.Hour), Now: now, Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, 2, report.Summary.Overdue)
	assert.Len(t, report.Sections.Overdue, 1)
}

func TestBuildRejectsInvalidScope(t *testing.T) {
	_, err := Build(nil, Options{Scope: "instance", Selector: "all"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid digest scope")
}

type issueOptions struct {
	state       string
	group       string
	priority    string
	targetDate  string
	updatedAt   time.Time
	completedAt *time.Time
	labels      []string
	assignees   []plane.FlexibleUser
}

func makeRecord(workspace, project string, sequence int, title string, opts issueOptions) Record {
	labels := make([]plane.FlexibleLabel, 0, len(opts.labels))
	for _, name := range opts.labels {
		labels = append(labels, plane.FlexibleLabel{Name: name})
	}

	projectData := plane.Project{ID: "project-" + project, Identifier: project, Name: project + " Project"}
	issue := plane.Issue{}
	issue.ID = "issue-id"
	issue.SequenceID = sequence
	issue.Name = title
	issue.State = plane.FlexibleState{Name: opts.state, Group: opts.group}
	issue.Priority = opts.priority
	issue.TargetDate = opts.targetDate
	issue.UpdatedAt = opts.updatedAt
	issue.CompletedAt = opts.completedAt
	issue.Labels = labels
	issue.Assignees = opts.assignees
	return Record{Workspace: workspace, Project: projectData, Issue: issue}
}
