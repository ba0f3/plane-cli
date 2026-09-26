package digest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancelledIsClosedButNotCompleted(t *testing.T) {
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	records := []Record{
		makeRecord("design", "DES", 1, "Cancelled", issueOptions{
			state:      "Cancelled",
			group:      "cancelled",
			targetDate: "2026-09-20",
			updatedAt:  now.Add(-time.Hour),
		}),
	}

	report, err := Build(records, Options{
		Scope:    "workspace",
		Selector: "design",
		Since:    now.Add(-24 * time.Hour),
		Now:      now,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, report.Summary.Matched)
	assert.Equal(t, 0, report.Summary.Active)
	assert.Equal(t, 0, report.Summary.CompletedRecent)
	assert.Equal(t, 0, report.Summary.Overdue)
	assert.Empty(t, report.Sections.Completed)
}
