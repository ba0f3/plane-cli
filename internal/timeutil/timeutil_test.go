package timeutil

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTime(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		input string
		want  string
	}{
		{"7d", "2026-11-16T12:00:00Z"},
		{"24h", "2026-11-22T12:00:00Z"},
		{"2026-11-01T00:00:00Z", "2026-11-01T00:00:00Z"},
		{"2026-11-01", "2026-11-01T00:00:00Z"},
	}
	for _, tt := range tests {
		got, err := ParseTime(tt.input, now)
		require.NoError(t, err, tt.input)
		assert.Equal(t, tt.want, got.UTC().Format(time.RFC3339), tt.input)
	}

	_, err := ParseTime("nonsense", now)
	assert.Error(t, err)
}
