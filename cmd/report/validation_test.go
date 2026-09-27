package report

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetReportValidationGlobals() {
	reportSince = ""
	reportUntil = ""
	reportDateField = "updated"
	workloadMetric = "count"
	workloadGroupBy = "assignee"
	activityLimit = 500
}

func TestValidateReportFiltersRejectsInvalidDateField(t *testing.T) {
	resetReportValidationGlobals()
	t.Cleanup(resetReportValidationGlobals)
	reportDateField = "completed"

	err := validateReportFilters(time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--date-field")
}

func TestValidateReportFiltersRejectsInvalidTime(t *testing.T) {
	resetReportValidationGlobals()
	t.Cleanup(resetReportValidationGlobals)
	reportSince = "not-a-time"

	err := validateReportFilters(time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--since")
}

func TestValidateReportFiltersRejectsReversedWindow(t *testing.T) {
	resetReportValidationGlobals()
	t.Cleanup(resetReportValidationGlobals)
	reportSince = "2026-09-27T12:00:00Z"
	reportUntil = "2026-09-26T12:00:00Z"

	err := validateReportFilters(time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--since is after --until")
}

func TestValidateWorkloadFlagsRejectsInvalidGroupBeforeScan(t *testing.T) {
	resetReportValidationGlobals()
	t.Cleanup(resetReportValidationGlobals)
	workloadGroupBy = "team"

	err := validateWorkloadFlags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--group-by")
}

func TestValidateActivityFlagsRejectsNegativeLimit(t *testing.T) {
	resetReportValidationGlobals()
	t.Cleanup(resetReportValidationGlobals)
	activityLimit = -1

	err := validateActivityFlags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--limit")
}

func TestIsOverdueDateUsesCalendarDate(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	require.NoError(t, err)
	now := time.Date(2026, 9, 27, 14, 0, 0, 0, loc)

	assert.True(t, isOverdueDate("2026-09-26", now))
	assert.False(t, isOverdueDate("2026-09-27", now), "due today must not be overdue")
	assert.False(t, isOverdueDate("2026-09-28", now))
	assert.False(t, isOverdueDate("invalid", now))
}
