package wiki

import (
	"testing"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummarizeWikiPagesRemovesContentFields(t *testing.T) {
	pages := []api.WikiPage{{
		"id":                   "page-1",
		"name":                 "Runbook",
		"description_html":     "<h1>large body</h1>",
		"description_json":     map[string]interface{}{"type": "doc"},
		"description_stripped": "large body",
		"description_markdown": "# large body",
		"updated_at":           "2026-09-27T00:00:00Z",
		"is_locked":            false,
	}}

	summaries := summarizeWikiPages(pages)
	require.Len(t, summaries, 1)
	summary := summaries[0]

	assert.Equal(t, "page-1", summary["id"])
	assert.Equal(t, "Runbook", summary["name"])
	assert.Contains(t, summary, "updated_at")
	assert.NotContains(t, summary, "description_html")
	assert.NotContains(t, summary, "description_json")
	assert.NotContains(t, summary, "description_stripped")
	assert.NotContains(t, summary, "description_markdown")
}
