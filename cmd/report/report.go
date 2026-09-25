package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/ba0f3/plane-cli/internal/output"
	"github.com/ba0f3/plane-cli/pkg/plane"
	"github.com/spf13/cobra"
)

var (
	reportProject   string
	reportSince     string
	reportUntil     string
	reportDateField string
	workloadMetric  string
	workloadGroupBy string
	digestLimit     int
)

var ReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Cross-workspace reports for PAT/WSAT/IAT",
}

type record struct {
	Workspace string
	Project   plane.Project
	Issue     plane.Issue
}

func init() {
	summary := &cobra.Command{Use: "summary", Short: "Workspace/project work-item summary", RunE: runSummary}
	workload := &cobra.Command{Use: "workload", Short: "Assignee workload and share", RunE: runWorkload}
	digest := &cobra.Command{Use: "digest", Short: "Recent work-item digest for cron/agents", RunE: runDigest}

	for _, cmd := range []*cobra.Command{summary, workload, digest} {
		cmd.Flags().StringVarP(&reportProject, "project", "p", "", "Project ID, identifier, or name")
		cmd.Flags().StringVar(&reportSince, "since", "", "Start time: RFC3339, YYYY-MM-DD, Go duration (24h), or Nd (7d)")
		cmd.Flags().StringVar(&reportUntil, "until", "", "End time: RFC3339 or YYYY-MM-DD")
		cmd.Flags().StringVar(&reportDateField, "date-field", "updated", "Date field: updated or created")
	}
	workload.Flags().StringVar(&workloadMetric, "metric", "count", "Workload metric: count or points")
	workload.Flags().StringVar(&workloadGroupBy, "group-by", "assignee", "Group by: assignee, workspace-assignee, project-assignee")
	digest.Flags().IntVarP(&digestLimit, "limit", "l", 500, "Maximum digest rows")

	ReportCmd.AddCommand(summary, workload, digest)
}

func collect() ([]record, error) {
	client, err := api.NewClientNoWorkspace()
	if err != nil {
		return nil, err
	}

	var workspaces []api.WorkspaceSummary
	if config.Cfg.DefaultWorkspace != "" {
		workspaces = []api.WorkspaceSummary{{Slug: config.Cfg.DefaultWorkspace, Name: config.Cfg.DefaultWorkspace}}
	} else {
		workspaces, err = client.ListWorkspaces()
		if err != nil {
			return nil, err
		}
	}

	var out []record
	for _, ws := range workspaces {
		scoped := client.CloneForWorkspace(ws.Slug)
		projects, err := scoped.ListProjects()
		if err != nil {
			return nil, fmt.Errorf("workspace %s: list projects: %w", ws.Slug, err)
		}
		for _, project := range projects {
			if reportProject != "" && !projectMatches(project, reportProject) {
				continue
			}
			issues, err := scoped.ListAllIssues(project.ID)
			if err != nil {
				return nil, fmt.Errorf("workspace %s project %s: %w", ws.Slug, project.Identifier, err)
			}
			for _, issue := range issues {
				if issueInWindow(issue) {
					out = append(out, record{Workspace: ws.Slug, Project: project, Issue: issue})
				}
			}
		}
	}
	return out, nil
}

func projectMatches(p plane.Project, selector string) bool {
	selector = strings.TrimSpace(selector)
	return strings.EqualFold(p.ID, selector) ||
		strings.EqualFold(p.Identifier, selector) ||
		strings.EqualFold(p.Name, selector)
}

func issueInWindow(issue plane.Issue) bool {
	var t time.Time
	switch strings.ToLower(reportDateField) {
	case "created":
		t = issue.CreatedAt
	case "updated", "":
		t = issue.UpdatedAt
	default:
		return false
	}
	if reportSince != "" {
		since, err := parseTime(reportSince, time.Now())
		if err != nil || t.Before(since) {
			return false
		}
	}
	if reportUntil != "" {
		until, err := parseTime(reportUntil, time.Now())
		if err != nil || t.After(until) {
			return false
		}
	}
	return true
}

func parseTime(v string, now time.Time) (time.Time, error) {
	v = strings.TrimSpace(v)
	if strings.HasSuffix(v, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(v, "d"))
		if err == nil && n >= 0 {
			return now.AddDate(0, 0, -n), nil
		}
	}
	if d, err := time.ParseDuration(v); err == nil {
		return now.Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", v, now.Location()); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q", v)
}

func stateGroup(issue plane.Issue) string {
	if issue.State.Group != "" {
		return strings.ToLower(issue.State.Group)
	}
	return strings.ToLower(issue.StateGroup)
}

func stateName(issue plane.Issue) string {
	if issue.State.Name != "" {
		return issue.State.Name
	}
	if issue.StateName != "" {
		return issue.StateName
	}
	return issue.State.ID
}

func assigneeNames(issue plane.Issue) []string {
	out := make([]string, 0, len(issue.Assignees))
	for _, a := range issue.Assignees {
		name := a.DisplayName
		if name == "" {
			name = strings.TrimSpace(a.FirstName + " " + a.LastName)
		}
		if name == "" {
			name = a.Email
		}
		if name == "" {
			name = a.ID
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func runSummary(cmd *cobra.Command, args []string) error {
	records, err := collect()
	if err != nil {
		return err
	}
	type agg struct {
		Workspace  string
		Project    string
		Items      int
		Active     int
		Done       int
		Overdue    int
		Unassigned int
		Points     int
	}
	m := map[string]*agg{}
	now := time.Now()
	for _, r := range records {
		key := r.Workspace + "\x00" + r.Project.Identifier
		a := m[key]
		if a == nil {
			a = &agg{Workspace: r.Workspace, Project: r.Project.Identifier}
			m[key] = a
		}
		a.Items++
		a.Points += r.Issue.EstimatePoint
		group := stateGroup(r.Issue)
		if group == "completed" || group == "cancelled" || group == "canceled" {
			a.Done++
		} else {
			a.Active++
		}
		if len(r.Issue.Assignees) == 0 {
			a.Unassigned++
		}
		if r.Issue.TargetDate != "" && a.Active > 0 {
			if due, err := time.Parse("2006-01-02", r.Issue.TargetDate); err == nil && due.Before(now) && group != "completed" && group != "cancelled" && group != "canceled" {
				a.Overdue++
			}
		}
	}
	type row struct {
		Workspace  string `table:"WORKSPACE" json:"workspace"`
		Project    string `table:"PROJECT" json:"project"`
		Items      int    `table:"ITEMS" json:"items"`
		Active     int    `table:"ACTIVE" json:"active"`
		Done       int    `table:"DONE" json:"done"`
		Completion string `table:"DONE %" json:"completion"`
		Overdue    int    `table:"OVERDUE" json:"overdue"`
		Unassigned int    `table:"UNASSIGNED" json:"unassigned"`
		Points     int    `table:"POINTS" json:"points"`
	}
	rows := make([]row, 0, len(m))
	for _, a := range m {
		pct := 0.0
		if a.Items > 0 {
			pct = float64(a.Done) * 100 / float64(a.Items)
		}
		rows = append(rows, row{
			Workspace: a.Workspace, Project: a.Project, Items: a.Items, Active: a.Active,
			Done: a.Done, Completion: fmt.Sprintf("%.1f%%", pct), Overdue: a.Overdue,
			Unassigned: a.Unassigned, Points: a.Points,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Workspace == rows[j].Workspace {
			return rows[i].Project < rows[j].Project
		}
		return rows[i].Workspace < rows[j].Workspace
	})
	return output.NewFormatter(config.Cfg.OutputFormat, false).Print(rows)
}

func runWorkload(cmd *cobra.Command, args []string) error {
	if workloadMetric != "count" && workloadMetric != "points" {
		return fmt.Errorf("invalid --metric %q; use count or points", workloadMetric)
	}
	records, err := collect()
	if err != nil {
		return err
	}
	type agg struct {
		Workspace string
		Project   string
		Assignee  string
		Items     int
		Points    int
		Value     float64
	}
	m := map[string]*agg{}
	for _, r := range records {
		names := assigneeNames(r.Issue)
		if len(names) == 0 {
			names = []string{"(unassigned)"}
		}
		for _, name := range names {
			var key string
			a := &agg{Assignee: name}
			switch workloadGroupBy {
			case "assignee":
				key = name
			case "workspace-assignee":
				key = r.Workspace + "\x00" + name
				a.Workspace = r.Workspace
			case "project-assignee":
				key = r.Workspace + "\x00" + r.Project.Identifier + "\x00" + name
				a.Workspace, a.Project = r.Workspace, r.Project.Identifier
			default:
				return fmt.Errorf("invalid --group-by %q", workloadGroupBy)
			}
			if old := m[key]; old != nil {
				a = old
			} else {
				m[key] = a
			}
			a.Items++
			a.Points += r.Issue.EstimatePoint
		}
	}
	total := 0.0
	for _, a := range m {
		if workloadMetric == "points" {
			a.Value = float64(a.Points)
		} else {
			a.Value = float64(a.Items)
		}
		total += a.Value
	}
	type row struct {
		Workspace string `table:"WORKSPACE" json:"workspace,omitempty"`
		Project   string `table:"PROJECT" json:"project,omitempty"`
		Assignee  string `table:"ASSIGNEE" json:"assignee"`
		Items     int    `table:"ITEMS" json:"items"`
		Points    int    `table:"POINTS" json:"points"`
		Share     string `table:"SHARE" json:"share"`
	}
	rows := make([]row, 0, len(m))
	for _, a := range m {
		share := 0.0
		if total > 0 {
			share = a.Value * 100 / total
		}
		rows = append(rows, row{
			Workspace: a.Workspace, Project: a.Project, Assignee: a.Assignee,
			Items: a.Items, Points: a.Points, Share: fmt.Sprintf("%.1f%%", share),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return strings.TrimSuffix(rows[i].Share, "%") > strings.TrimSuffix(rows[j].Share, "%")
	})
	return output.NewFormatter(config.Cfg.OutputFormat, false).Print(rows)
}

func runDigest(cmd *cobra.Command, args []string) error {
	if reportSince == "" {
		reportSince = "24h"
	}
	records, err := collect()
	if err != nil {
		return err
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Issue.UpdatedAt.After(records[j].Issue.UpdatedAt)
	})
	if digestLimit > 0 && len(records) > digestLimit {
		records = records[:digestLimit]
	}
	type row struct {
		Workspace string   `table:"WORKSPACE" json:"workspace"`
		Project   string   `table:"PROJECT" json:"project"`
		Issue     string   `table:"ISSUE" json:"issue"`
		Title     string   `table:"TITLE" json:"title"`
		State     string   `table:"STATE" json:"state"`
		Priority  string   `table:"PRIORITY" json:"priority"`
		Assignees []string `table:"ASSIGNEES" json:"assignees"`
		Target    string   `table:"TARGET" json:"target_date,omitempty"`
		UpdatedAt string   `table:"UPDATED" json:"updated_at"`
	}
	rows := make([]row, 0, len(records))
	for _, r := range records {
		id := fmt.Sprintf("%s-%d", r.Project.Identifier, r.Issue.SequenceID)
		rows = append(rows, row{
			Workspace: r.Workspace, Project: r.Project.Identifier, Issue: id,
			Title: r.Issue.Name, State: stateName(r.Issue), Priority: r.Issue.Priority,
			Assignees: assigneeNames(r.Issue), Target: r.Issue.TargetDate,
			UpdatedAt: r.Issue.UpdatedAt.Format(time.RFC3339),
		})
	}
	return output.NewFormatter(config.Cfg.OutputFormat, false).Print(rows)
}
