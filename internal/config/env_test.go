package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitConfigEnvironmentOverrides(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("api_host: https://from-file.example\noutput_format: yaml\n"), 0644))
	SetConfigFile(configPath)

	oldCfg := Cfg
	t.Cleanup(func() { Cfg = oldCfg })

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	require.NoError(t, os.Chdir(tempDir))

	t.Setenv("PLANE_API_HOST", "https://plane.internal.example")
	t.Setenv("PLANE_BASE_URL", "https://legacy.example")
	t.Setenv("PLANE_WORKSPACE", "engineering")
	t.Setenv("PLANE_PROJECT", "proj-1")

	require.NoError(t, InitConfig())
	assert.Equal(t, "https://plane.internal.example", Cfg.APIHost)
	assert.Equal(t, "engineering", Cfg.DefaultWorkspace)
	assert.Equal(t, "proj-1", Cfg.DefaultProject)
}

func TestInitConfigLegacyBaseURLAlias(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("output_format: yaml\n"), 0644))
	SetConfigFile(configPath)

	oldCfg := Cfg
	t.Cleanup(func() { Cfg = oldCfg })

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	require.NoError(t, os.Chdir(tempDir))

	t.Setenv("PLANE_API_HOST", "")
	t.Setenv("PLANE_BASE_URL", "https://legacy.example")

	require.NoError(t, InitConfig())
	assert.Equal(t, "https://legacy.example", Cfg.APIHost)
}
