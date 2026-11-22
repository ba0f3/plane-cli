package viewhint

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useTempCache(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	original := cacheDir
	cacheDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { cacheDir = original })
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

	warn, err = Check("ws", "p1", start.Add(6*time.Minute))
	require.NoError(t, err)
	assert.True(t, warn)
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
	original := cacheDir
	cacheDir = func() (string, error) { return "", fmt.Errorf("no cache dir") }
	t.Cleanup(func() { cacheDir = original })

	warn, err := Check("ws", "p1", time.Now())
	assert.Error(t, err)
	assert.False(t, warn)
}
