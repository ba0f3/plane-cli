package issue

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/ba0f3/plane-cli/internal/viewhint"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunViewFailedViewsDoNotRecordViewHints proves that a failing issue view
// does not count towards the view-hint threshold: no viewhint event may be
// written when the issue could not be resolved.
func TestRunViewFailedViewsDoNotRecordViewHints(t *testing.T) {
	cacheDir := t.TempDir()
	originalCacheDir := viewhint.CacheDir
	viewhint.CacheDir = func() (string, error) { return cacheDir, nil }
	t.Cleanup(func() { viewhint.CacheDir = originalCacheDir })

	originalHost := config.Cfg.APIHost
	originalWorkspace := config.Cfg.DefaultWorkspace
	originalProject := config.Cfg.DefaultProject
	t.Cleanup(func() {
		config.Cfg.APIHost = originalHost
		config.Cfg.DefaultWorkspace = originalWorkspace
		config.Cfg.DefaultProject = originalProject
	})

	// Point the API at a closed port so every request fails.
	config.Cfg.APIHost = "http://127.0.0.1:1"
	config.Cfg.DefaultWorkspace = "ws"
	config.Cfg.DefaultProject = "proj"

	t.Setenv("PLANE_API_KEY", "test-key")

	for i := 0; i < 5; i++ {
		var out bytes.Buffer
		cmd := &cobra.Command{Use: "view"}
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		err := runView(cmd, []string{"1"})
		require.Error(t, err, "view against an unreachable API must fail")
	}

	hintsPath := filepath.Join(cacheDir, "plane-cli", "view-hints.jsonl")
	assert.NoFileExists(t, hintsPath, "failed issue views must not record viewhint events")

	// The cache directory must not even have been created for the hints file.
	entries, err := os.ReadDir(filepath.Join(cacheDir, "plane-cli"))
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err)
	for _, entry := range entries {
		assert.NotEqual(t, "view-hints.jsonl", entry.Name())
	}
}
