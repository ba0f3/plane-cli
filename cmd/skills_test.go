package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderSkillDescribesRunningCLI(t *testing.T) {
	skill := renderSkill(rootCmd)

	assert.Contains(t, skill, "---\nname: plane-cli\n")
	assert.Contains(t, skill, "# Plane CLI Agent Skill")
	assert.Contains(t, skill, "PLANE_API_HOST")
	assert.Contains(t, skill, "### `plane-cli auth context`")
	assert.Contains(t, skill, "### `plane-cli workspace list`")
	assert.Contains(t, skill, "### `plane-cli project list`")
	assert.Contains(t, skill, "### `plane-cli report summary`")
	assert.Contains(t, skill, "### `plane-cli digest`")
	assert.Contains(t, skill, "### `plane-cli admin`")
	assert.Contains(t, skill, "### `plane-cli wiki create`")
	assert.Contains(t, skill, "`--file`: Read Markdown page content from a UTF-8 file")
	assert.Contains(t, skill, "### `plane-cli raw`")
	assert.Contains(t, skill, "### `plane-cli skills`")
	assert.Contains(t, skill, "`-o, --output`: Output format: json, yaml")
}

func TestSkillsCommandDoesNotRequireConfig(t *testing.T) {
	badConfig := filepath.Join(t.TempDir(), "broken.yaml")
	require.NoError(t, os.WriteFile(badConfig, []byte(": invalid yaml: ["), 0o600))

	originalConfigFile := configFile
	configFile = badConfig
	t.Cleanup(func() { configFile = originalConfigFile })

	cmd, _, err := rootCmd.Find([]string{"skills"})
	require.NoError(t, err)
	require.NotNil(t, cmd)
	require.NoError(t, rootCmd.PersistentPreRunE(cmd, nil))
}

func TestSkillsCommandWritesMarkdown(t *testing.T) {
	var out bytes.Buffer
	originalOut := skillsCmd.OutOrStdout()
	skillsCmd.SetOut(&out)
	t.Cleanup(func() { skillsCmd.SetOut(originalOut) })

	require.NoError(t, skillsCmd.RunE(skillsCmd, nil))
	assert.True(t, bytes.HasPrefix(out.Bytes(), []byte("---\nname: plane-cli\n")))
	assert.Contains(t, out.String(), "## Command reference")
}
