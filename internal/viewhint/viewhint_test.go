package viewhint

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useTempCache(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	original := CacheDir
	CacheDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { CacheDir = original })
}

func TestCheckWarnsAtThresholdOncePerWindow(t *testing.T) {
	useTempCache(t)
	start := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	for i := 0; i < Threshold-1; i++ {
		warn, err := Check("ws", "p1", start.Add(time.Duration(i)*time.Minute))
		require.NoError(t, err)
		assert.False(t, warn)
	}
	warn, err := Check("ws", "p1", start.Add(2*time.Minute))
	require.NoError(t, err)
	assert.True(t, warn)

	warn, err = Check("ws", "p1", start.Add(3*time.Minute))
	require.NoError(t, err)
	assert.False(t, warn)

	// The warning recorded at t=2min is still inside the 5-minute window that
	// ends at t=6min, so the hint stays suppressed for that window.
	warn, err = Check("ws", "p1", start.Add(6*time.Minute))
	require.NoError(t, err)
	assert.False(t, warn)

	// Once the t=2min warning's window has fully passed (at t=7min the cutoff
	// reaches 12:02, so that warning has expired), the hint fires again: the
	// views at 3min, 6min and 7min are all inside the new window.
	warn, err = Check("ws", "p1", start.Add(7*time.Minute))
	require.NoError(t, err)
	assert.True(t, warn)

	// The warning just recorded at t=7min suppresses the hint for the rest of
	// its own 5-minute window.
	warn, err = Check("ws", "p1", start.Add(8*time.Minute))
	require.NoError(t, err)
	assert.False(t, warn)
}

func TestCheckIsolatesKeys(t *testing.T) {
	useTempCache(t)
	start := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	for i := 0; i < Threshold; i++ {
		_, err := Check("ws", "p1", start.Add(time.Duration(i)*time.Minute))
		require.NoError(t, err)
	}
	warn, err := Check("ws", "p2", start.Add(3*time.Minute))
	require.NoError(t, err)
	assert.False(t, warn)
}

func TestCheckReportsCacheFailure(t *testing.T) {
	original := CacheDir
	CacheDir = func() (string, error) { return "", fmt.Errorf("no cache dir") }
	t.Cleanup(func() { CacheDir = original })

	warn, err := Check("ws", "p1", time.Now())
	assert.Error(t, err)
	assert.False(t, warn)
}

func TestCheckKeepsCacheFileBounded(t *testing.T) {
	useTempCache(t)
	start := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	// Drive far more view calls than the cache may hold.
	for i := 0; i < maxEvents*3; i++ {
		_, err := Check("ws", "p1", start.Add(time.Duration(i)*time.Second))
		require.NoError(t, err)
	}

	path, err := eventsPath()
	require.NoError(t, err)
	lines := countLines(t, path)
	assert.LessOrEqual(t, lines, maxEvents, "cache file must stay bounded")
	assert.Greater(t, lines, 0, "cache file must not be emptied")
}

func TestCheckKeepsCacheFileBoundedAtWarnBoundary(t *testing.T) {
	useTempCache(t)
	start := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	path, err := eventsPath()
	require.NoError(t, err)

	// Fill the file right up to the bound so the next calls straddle the
	// boundary where a warn event is appended after the prune. The file must
	// never hold more than maxEvents lines, not even transiently.
	for i := 0; i < maxEvents-1; i++ {
		_, err := Check("ws", "p1", start.Add(time.Duration(i)*time.Millisecond))
		require.NoError(t, err)
	}
	require.LessOrEqual(t, countLines(t, path), maxEvents)

	for i := 0; i < 10; i++ {
		_, err := Check("ws", "p1", start.Add(time.Duration(maxEvents+i)*time.Millisecond))
		require.NoError(t, err)
		assert.LessOrEqual(t, countLines(t, path), maxEvents,
			"cache file must stay bounded even when a warn event is appended")
	}
}

func TestCheckPruneKeepsRecentWarningSuppression(t *testing.T) {
	useTempCache(t)
	start := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	// Two views are below the threshold; the third one fires the warning.
	for i := 0; i < Threshold-1; i++ {
		warn, err := Check("ws", "p1", start.Add(time.Duration(i)*time.Millisecond))
		require.NoError(t, err)
		assert.False(t, warn)
	}
	warn, err := Check("ws", "p1", start.Add(2*time.Millisecond))
	require.NoError(t, err)
	assert.True(t, warn)

	// Keep adding views so the file crosses the prune threshold. All calls stay
	// inside the same 5-minute window as the recorded warning.
	for i := 0; i < maxEvents*2; i++ {
		_, err := Check("ws", "p1", start.Add(time.Duration(10+i)*time.Millisecond))
		require.NoError(t, err)
	}

	path, err := eventsPath()
	require.NoError(t, err)
	assert.LessOrEqual(t, countLines(t, path), maxEvents)

	// The warning recorded inside the current window must still suppress a
	// repeated hint after the prune.
	warn, err = Check("ws", "p1", start.Add(time.Duration(20+maxEvents*2)*time.Millisecond))
	require.NoError(t, err)
	assert.False(t, warn)
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}
