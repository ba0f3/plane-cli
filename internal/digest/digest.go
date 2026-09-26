package digest

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ba0f3/plane-cli/pkg/plane"
)

const SchemaVersion = "1"

type Record struct {
	Workspace string
	Project   plane.Project
	Issue     plane.Issue
}

type Options struct {
	Scope     string
	Selector  string
	Since     time.Time
	Now       time.Time
	StaleDays int
	Limit     int
}

type ScopeInfo struct {
	Type     string `json:"type"`
	Selector string `json:"selector"`
}

type WindowInfo struct {
	CompletedSince string `json:"completed_since"`
	StaleDays      int    `json:"stale_days"`
}

type Summary struct {
	Matched         int `json:"matched"`
	Active          int `json:"active"`
	Overdue         int `json:"overdue"`
	DueToday        int `json:"due_today"`
	Blocked         int `json:"blocked"`
	Stale           int `json:"stale"`
	CompletedRecent int `json:"completed_recent"`
	Unassigned      int `json:"unassigned"`
}

type Assignee struct {
	ID    string `json:"id,omitempty"`
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
}

type Item struct {
	Workspace   string     `json:"workspace"`
	Project     string     `json:"project"`
	ProjectID   string     `json:"project_id"`
	Issue       string     `json:"issue"`
	IssueID     string     `json:"issue_id"`
	Title       string     `json:"title"`
	State       string     `json:"state"`
	StateGroup  string     `json:"state_group,omitempty"`
	Priority    string     `json:"priority,omitempty"`
	Assignees   []Assignee `json:"assignees"`
	Labels      []string   `json:"labels,omitempty"`
	StartDate   string     `json:"start_date,omitempty"`
	TargetDate  string     `json:"target_date,omitempty"`
	UpdatedAt   string     `json:"updated_at"`
	CompletedAt string     `json:"completed_at,omitempty"`
}

type Sections struct {
	Overdue    []Item `json:"overdue"`
	DueToday   []Item `json:"due_today"`
	Blocked    []Item `json:"blocked"`
	Stale      []Item `json:"stale"`
	Completed  []Item `json:"completed"`
	Unassigned []Item `json:"unassigned"`
}

type Report struct {
	SchemaVersion string     `json:"schema_version"`
	GeneratedAt   string     `json:"generated_at"`
	Scope         ScopeInfo  `json:"scope"`
	Window        WindowInfo `json:"window"`
	Summary       Summary    `json:"summary"`
	Sections      Sections   `json:"sections"`
}

func Build(records []Record, opts Options) (Report, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.Since.IsZero() {
		opts.Since = opts.Now.Add(-24 * time.Hour)
	}
	if opts.StaleDays <= 0 {
		opts.StaleDays = 7
	}
	if opts.Limit < 0 {
		return Report{}, fmt.Errorf("limit must be >= 0")
	}
	if err := validateScope(opts.Scope, opts.Selector); err != nil {
		return Report{}, err
	}

	report := Report{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   opts.Now.Format(time.RFC3339),
		Scope: ScopeInfo{
			Type:     opts.Scope,
			Selector: opts.Selector,
		},
		Window: WindowInfo{
			CompletedSince: opts.Since.Format(time.RFC3339),
			StaleDays:      opts.StaleDays,
		},
		Sections: Sections{
			Overdue:    []Item{},
			DueToday:   []Item{},
			Blocked:    []Item{},
			Stale:      []Item{},
			Completed:  []Item{},
			Unassigned: []Item{},
		},
	}

	today := day(opts.Now)
	staleBefore := opts.Now.AddDate(0, 0, -opts.StaleDays)

	for _, r := range records {
		if !matchesScope(r, opts.Scope, opts.Selector) {
			continue
		}
		report.Summary.Matched++
		item := toItem(r)
		group := normalizedStateGroup(r.Issue)

		if isClosedGroup(group) {
			if group == "completed" {
				completedAt := completionTime(r.Issue)
				if !completedAt.IsZero() && !completedAt.Before(opts.Since) && !completedAt.After(opts.Now) {
					report.Summary.CompletedRecent++
					report.Sections.Completed = append(report.Sections.Completed, item)
				}
			}
			continue
		}

		report.Summary.Active++
		if len(r.Issue.Assignees) == 0 {
			report.Summary.Unassigned++
			report.Sections.Unassigned = append(report.Sections.Unassigned, item)
		}
		if isBlocked(r.Issue) {
			report.Summary.Blocked++
			report.Sections.Blocked = append(report.Sections.Blocked, item)
		}
		if !r.Issue.UpdatedAt.IsZero() && r.Issue.UpdatedAt.Before(staleBefore) {
			report.Summary.Stale++
			report.Sections.Stale = append(report.Sections.Stale, item)
		}
		if due, ok := parseDate(r.Issue.TargetDate, opts.Now.Location()); ok {
			switch {
			case due.Before(today):
				report.Summary.Overdue++
				report.Sections.Overdue = append(report.Sections.Overdue, item)
			case due.Equal(today):
				report.Summary.DueToday++
				report.Sections.DueToday = append(report.Sections.DueToday, item)
			}
		}
	}

	sortSections(&report.Sections)
	limitSections(&report.Sections, opts.Limit)
	return report, nil
}

func validateScope(scope, selector string) error {
	switch scope {
	case "user", "project", "workspace":
	default:
		return fmt.Errorf("invalid digest scope %q", scope)
	}
	if strings.TrimSpace(selector) == "" {
		return fmt.Errorf("digest %s selector is required", scope)
	}
	return nil
}

func matchesScope(r Record, scope, selector string) bool {
	selector = strings.TrimSpace(selector)
	switch scope {
	case "workspace":
		return strings.EqualFold(r.Workspace, selector)
	case "project":
		return strings.EqualFold(r.Project.ID, selector) ||
			strings.EqualFold(r.Project.Identifier, selector) ||
			strings.EqualFold(r.Project.Name, selector)
	case "user":
		for _, a := range r.Issue.Assignees {
			if userMatches(a, selector) {
				return true
			}
		}
	}
	return false
}

func userMatches(user plane.FlexibleUser, selector string) bool {
	fullName := strings.TrimSpace(user.FirstName + " " + user.LastName)
	return strings.EqualFold(user.ID, selector) ||
		strings.EqualFold(user.Email, selector) ||
		strings.EqualFold(user.DisplayName, selector) ||
		strings.EqualFold(fullName, selector)
}

func normalizedStateGroup(issue plane.Issue) string {
	if issue.State.Group != "" {
		return strings.ToLower(strings.TrimSpace(issue.State.Group))
	}
	return strings.ToLower(strings.TrimSpace(issue.StateGroup))
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

func isClosedGroup(group string) bool {
	return group == "completed" || group == "cancelled" || group == "canceled"
}

func isBlocked(issue plane.Issue) bool {
	if containsBlockedToken(stateName(issue)) || containsBlockedToken(normalizedStateGroup(issue)) {
		return true
	}
	for _, label := range issue.Labels {
		if containsBlockedToken(label.Name) {
			return true
		}
	}
	return false
}

func containsBlockedToken(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "block" || value == "blocked" || value == "blocker"
}

func completionTime(issue plane.Issue) time.Time {
	if issue.CompletedAt != nil {
		return *issue.CompletedAt
	}
	return issue.UpdatedAt
}

func parseDate(value string, loc *time.Location) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", value, loc)
	return t, err == nil
}

func day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func toItem(r Record) Item {
	assignees := make([]Assignee, 0, len(r.Issue.Assignees))
	for _, a := range r.Issue.Assignees {
		name := a.DisplayName
		if name == "" {
			name = strings.TrimSpace(a.FirstName + " " + a.LastName)
		}
		assignees = append(assignees, Assignee{ID: a.ID, Email: a.Email, Name: name})
	}
	labels := make([]string, 0, len(r.Issue.Labels))
	for _, label := range r.Issue.Labels {
		if label.Name != "" {
			labels = append(labels, label.Name)
		}
	}
	completedAt := ""
	if r.Issue.CompletedAt != nil {
		completedAt = r.Issue.CompletedAt.Format(time.RFC3339)
	}
	return Item{
		Workspace:   r.Workspace,
		Project:     r.Project.Identifier,
		ProjectID:   r.Project.ID,
		Issue:       fmt.Sprintf("%s-%d", r.Project.Identifier, r.Issue.SequenceID),
		IssueID:     r.Issue.ID,
		Title:       r.Issue.Name,
		State:       stateName(r.Issue),
		StateGroup:  normalizedStateGroup(r.Issue),
		Priority:    r.Issue.Priority,
		Assignees:   assignees,
		Labels:      labels,
		StartDate:   r.Issue.StartDate,
		TargetDate:  r.Issue.TargetDate,
		UpdatedAt:   r.Issue.UpdatedAt.Format(time.RFC3339),
		CompletedAt: completedAt,
	}
}

func sortSections(sections *Sections) {
	sort.Slice(sections.Overdue, func(i, j int) bool {
		if sections.Overdue[i].TargetDate == sections.Overdue[j].TargetDate {
			return higherPriority(sections.Overdue[i], sections.Overdue[j])
		}
		return sections.Overdue[i].TargetDate < sections.Overdue[j].TargetDate
	})
	sort.Slice(sections.DueToday, func(i, j int) bool {
		return higherPriority(sections.DueToday[i], sections.DueToday[j])
	})
	sort.Slice(sections.Blocked, func(i, j int) bool {
		return higherPriority(sections.Blocked[i], sections.Blocked[j])
	})
	sort.Slice(sections.Stale, func(i, j int) bool {
		return sections.Stale[i].UpdatedAt < sections.Stale[j].UpdatedAt
	})
	sort.Slice(sections.Completed, func(i, j int) bool {
		return completionSortValue(sections.Completed[i]) > completionSortValue(sections.Completed[j])
	})
	sort.Slice(sections.Unassigned, func(i, j int) bool {
		return higherPriority(sections.Unassigned[i], sections.Unassigned[j])
	})
}

func priorityRank(priority string) int {
	switch strings.ToLower(priority) {
	case "urgent":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	case "none", "":
		return 1
	default:
		return 0
	}
}

func higherPriority(left, right Item) bool {
	leftRank := priorityRank(left.Priority)
	rightRank := priorityRank(right.Priority)
	if leftRank == rightRank {
		return left.Issue < right.Issue
	}
	return leftRank > rightRank
}

func completionSortValue(item Item) string {
	if item.CompletedAt != "" {
		return item.CompletedAt
	}
	return item.UpdatedAt
}

func limitSections(sections *Sections, limit int) {
	if limit == 0 {
		return
	}
	sections.Overdue = limitItems(sections.Overdue, limit)
	sections.DueToday = limitItems(sections.DueToday, limit)
	sections.Blocked = limitItems(sections.Blocked, limit)
	sections.Stale = limitItems(sections.Stale, limit)
	sections.Completed = limitItems(sections.Completed, limit)
	sections.Unassigned = limitItems(sections.Unassigned, limit)
}

func limitItems(items []Item, limit int) []Item {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}
