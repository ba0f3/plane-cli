package issue

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/ba0f3/plane-cli/internal/output"
	"github.com/ba0f3/plane-cli/pkg/plane"
	"github.com/spf13/cobra"
)

var allProjects bool

type workspaceIssueRecord struct {
	Project plane.Project
	Issue   plane.Issue
}

type workspaceIssueOutput struct {
	Project   string            `table:"PROJECT" json:"project"`
	Key       string            `table:"ISSUE" json:"key"`
	ID        string            `table:"ID" json:"id"`
	Sequence  int               `table:"#" json:"sequence_id"`
	Title     string            `table:"TITLE" json:"title"`
	State     plane.StateOutput `table:"STATE" json:"state"`
	Priority  string            `table:"PRIORITY" json:"priority"`
	Assignees []string          `table:"ASSIGNEES" json:"assignees"`
	UpdatedAt string            `table:"UPDATED" json:"updated_at"`
}

func init() {
	listCmd.Flags().BoolVar(&allProjects, "all-projects", false, "List issues across all visible projects in the workspace")

	projectScopedRun := listCmd.RunE
	listCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if allProjects || strings.TrimSpace(config.Cfg.DefaultProject) == "" {
			return runWorkspaceList(cmd, args)
		}
		return projectScopedRun(cmd, args)
	}
}

func runWorkspaceList(cmd *cobra.Command, args []string) error {
	if perPage < 0 {
		return fmt.Errorf("--limit must be >= 0")
	}

	client, err := api.NewClient()
	if err != nil {
		return err
	}

	records, err := collectWorkspaceIssues(client, stateFilter, assigneeFilter, listAll)
	if err != nil {
		return err
	}

	sortWorkspaceIssues(records)
	if !listAll && perPage > 0 && len(records) > perPage {
		records = records[:perPage]
	}

	if len(records) == 0 {
		output.Info("No issues found")
		return nil
	}

	rows := make([]workspaceIssueOutput, 0, len(records))
	for _, record := range records {
		rows = append(rows, workspaceIssueOutput{
			Project:   projectDisplay(record.Project),
			Key:       issueKey(record.Project, record.Issue),
			ID:        record.Issue.ID,
			Sequence:  record.Issue.SequenceID,
			Title:     record.Issue.Name,
			State:     plane.StateOutputFromIssue(record.Issue),
			Priority:  record.Issue.Priority,
			Assignees: workspaceAssigneeNames(record.Issue),
			UpdatedAt: formatIssueTime(record.Issue.UpdatedAt),
		})
	}

	return output.NewFormatter(config.Cfg.OutputFormat, false).Print(rows)
}

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

func workspaceIssueMatches(issue plane.Issue, stateID, assigneeID string) bool {
	stateID = strings.TrimSpace(stateID)
	if stateID != "" && !strings.EqualFold(strings.TrimSpace(issue.State.ID), stateID) {
		return false
	}

	assigneeID = strings.TrimSpace(assigneeID)
	if assigneeID == "" {
		return true
	}
	for _, assignee := range issue.Assignees {
		if strings.EqualFold(strings.TrimSpace(assignee.ID), assigneeID) {
			return true
		}
	}
	return false
}

func sortWorkspaceIssues(records []workspaceIssueRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		left := records[i]
		right := records[j]
		if !left.Issue.UpdatedAt.Equal(right.Issue.UpdatedAt) {
			return left.Issue.UpdatedAt.After(right.Issue.UpdatedAt)
		}
		if left.Project.Identifier != right.Project.Identifier {
			return left.Project.Identifier < right.Project.Identifier
		}
		return left.Issue.SequenceID < right.Issue.SequenceID
	})
}

func projectDisplay(project plane.Project) string {
	if strings.TrimSpace(project.Identifier) != "" {
		return project.Identifier
	}
	if strings.TrimSpace(project.Name) != "" {
		return project.Name
	}
	return project.ID
}

func issueKey(project plane.Project, issue plane.Issue) string {
	identifier := strings.TrimSpace(project.Identifier)
	if identifier == "" || issue.SequenceID <= 0 {
		return issue.ID
	}
	return fmt.Sprintf("%s-%d", identifier, issue.SequenceID)
}

func workspaceAssigneeNames(issue plane.Issue) []string {
	out := make([]string, 0, len(issue.Assignees))
	for _, assignee := range issue.Assignees {
		name := strings.TrimSpace(assignee.DisplayName)
		if name == "" {
			name = strings.TrimSpace(assignee.FirstName + " " + assignee.LastName)
		}
		if name == "" {
			name = strings.TrimSpace(assignee.Email)
		}
		if name == "" {
			name = strings.TrimSpace(assignee.ID)
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func formatIssueTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}
