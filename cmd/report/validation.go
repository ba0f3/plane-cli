package report

import (
	"fmt"
	"strings"
	"time"
)

func validateReportFilters(now time.Time) error {
	dateField := strings.ToLower(strings.TrimSpace(reportDateField))
	switch dateField {
	case "", "updated", "created":
	default:
		return fmt.Errorf("invalid --date-field %q; use updated or created", reportDateField)
	}

	var since, until time.Time
	var err error
	if strings.TrimSpace(reportSince) != "" {
		since, err = parseTime(reportSince, now)
		if err != nil {
			return fmt.Errorf("invalid --since: %w", err)
		}
	}
	if strings.TrimSpace(reportUntil) != "" {
		until, err = parseTime(reportUntil, now)
		if err != nil {
			return fmt.Errorf("invalid --until: %w", err)
		}
	}
	if !since.IsZero() && !until.IsZero() && since.After(until) {
		return fmt.Errorf("invalid report window: --since is after --until")
	}
	return nil
}

func validateWorkloadFlags() error {
	switch workloadMetric {
	case "count", "points":
	default:
		return fmt.Errorf("invalid --metric %q; use count or points", workloadMetric)
	}

	switch workloadGroupBy {
	case "assignee", "workspace-assignee", "project-assignee":
	default:
		return fmt.Errorf("invalid --group-by %q; use assignee, workspace-assignee, or project-assignee", workloadGroupBy)
	}
	return nil
}

func validateActivityFlags() error {
	if activityLimit < 0 {
		return fmt.Errorf("invalid --limit %d; use 0 for unlimited or a positive value", activityLimit)
	}
	return nil
}

func isOverdueDate(targetDate string, now time.Time) bool {
	if strings.TrimSpace(targetDate) == "" {
		return false
	}
	due, err := time.ParseInLocation("2006-01-02", targetDate, now.Location())
	if err != nil {
		return false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return due.Before(today)
}
