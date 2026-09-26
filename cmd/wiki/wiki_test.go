package wiki

import (
	"strings"
	"testing"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummarizeWikiPagesRemovesContent(t *testing.T) {
	pages := []api.WikiPage{{
		"id":               "page-1",
		"name":             "Runbook",
		"description_html": "<p>very long</p>",
		"description_json": map[string]interface{}{"type": "doc"},
		"content":          "legacy content",
		"updated_at":       "2026-09-27T00:00:00Z",
	}}

	out := summarizeWikiPages(pages)
	require.Len(t, out, 1)
	assert.Equal(t, "page-1", out[0]["id"])
	assert.Equal(t, "Runbook", out[0]["name"])
	assert.Equal(t, "2026-09-27T00:00:00Z", out[0]["updated_at"])
	assert.NotContains(t, out[0], "description_html")
	assert.NotContains(t, out[0], "description_json")
	assert.NotContains(t, out[0], "content")
}

func TestPageBodyHTMLMarkdown(t *testing.T) {
	html, set, err := pageBodyHTML("# Runbook\n\n**Important**", true, "", false, "", false, strings.NewReader(""))
	require.NoError(t, err)
	assert.True(t, set)
	assert.Contains(t, html, "<h1>Runbook</h1>")
	assert.Contains(t, html, "<strong>Important</strong>")
}

func TestPageBodyHTMLFromFile(t *testing.T) {
	path := t.TempDir() + "/runbook.md"
	require.NoError(t, osWriteFile(path, []byte("## Deploy\n\n- build\n- ship")))

	html, set, err := pageBodyHTML("", false, path, true, "", false, strings.NewReader(""))
	require.NoError(t, err)
	assert.True(t, set)
	assert.Contains(t, html, "<h2>Deploy</h2>")
	assert.Contains(t, html, "<li>build</li>")
}

func TestPageBodyHTMLFromStdin(t *testing.T) {
	html, set, err := pageBodyHTML("", false, "-", true, "", false, strings.NewReader("### Piped"))
	require.NoError(t, err)
	assert.True(t, set)
	assert.Contains(t, html, "<h3>Piped</h3>")
}

func TestPageBodyHTMLRawHTML(t *testing.T) {
	html, set, err := pageBodyHTML("", false, "", false, "<p>raw</p>", true, strings.NewReader(""))
	require.NoError(t, err)
	assert.True(t, set)
	assert.Equal(t, "<p>raw</p>", html)
}

func TestPageBodyHTMLRejectsMultipleSources(t *testing.T) {
	_, _, err := pageBodyHTML("hello", true, "page.md", true, "", false, strings.NewReader(""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only one")
}

func TestPageBodyHTMLNoSource(t *testing.T) {
	html, set, err := pageBodyHTML("", false, "", false, "", false, strings.NewReader(""))
	require.NoError(t, err)
	assert.False(t, set)
	assert.Empty(t, html)
}

func osWriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
