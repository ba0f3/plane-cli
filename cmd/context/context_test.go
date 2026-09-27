package context

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtendedCommandsCoverModernFamilies(t *testing.T) {
	content := getExtendedCommands()

	assert.Contains(t, content, "plane-cli report summary")
	assert.Contains(t, content, "plane-cli digest user")
	assert.Contains(t, content, "plane-cli wiki list")
	assert.Contains(t, content, "plane-cli admin token")
	assert.Contains(t, content, "plane-cli raw <METHOD> <PATH>")
	assert.Contains(t, content, "plane-cli skills")
}
