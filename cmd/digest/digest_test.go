package digest

import (
	"testing"
	"time"

	"github.com/ba0f3/plane-cli/pkg/plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTime(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

	parsed, err := parseTime("7d", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC), parsed)

	parsed, err = parseTime("24h", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), parsed)

	parsed, err = parseTime("2026-09-20", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), parsed)
}

func TestParseLocation(t *testing.T) {
	location, err := parseLocation("UTC")
	require.NoError(t, err)
	assert.Equal(t, "UTC", location.String())

	_, err = parseLocation("Not/A_Real_Timezone")
	require.Error(t, err)
}

func TestProjectMatches(t *testing.T) {
	project := plane.Project{ID: "uuid-1", Identifier: "GAME", Name: "Game Team"}
	assert.True(t, projectMatches(project, "uuid-1"))
	assert.True(t, projectMatches(project, "game"))
	assert.True(t, projectMatches(project, "game team"))
	assert.False(t, projectMatches(project, "WEB"))
}
