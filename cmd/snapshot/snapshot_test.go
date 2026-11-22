package snapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ba0f3/plane-cli/internal/config"
	snap "github.com/ba0f3/plane-cli/internal/snapshot"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRejectsAllWorkspacesWithWorkspace(t *testing.T) {
	root := &cobra.Command{Use: "plane-cli"}
	root.PersistentFlags().String("workspace", "", "")
	sub := &cobra.Command{Use: "snapshot"}
	root.AddCommand(sub)
	require.NoError(t, root.PersistentFlags().Set("workspace", "ws"))

	allWorkspaces = true
	t.Cleanup(func() { allWorkspaces = false })

	err := validate(sub)
	assert.ErrorContains(t, err, "--all-workspaces")
}

func TestValidateRejectsInvalidDateField(t *testing.T) {
	dateField = "completed"
	t.Cleanup(func() { dateField = "updated" })

	err := validate(&cobra.Command{Use: "snapshot"})
	assert.ErrorContains(t, err, "date field")
}

func TestWriteSnapshotCreatesAtomicFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "snapshot.json")
	s := &snap.Snapshot{
		StartedAt: "2026-11-23T00:00:00Z", FinishedAt: "2026-11-23T00:01:00Z",
		Scope: snap.Scope{Workspace: "ws"}, Coverage: []snap.ProjectCoverage{}, Records: []snap.Record{},
	}

	require.NoError(t, writeSnapshot(path, s))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var decoded snap.Snapshot
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "ws", decoded.Scope.Workspace)
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestRunSnapshotWritesFileAndSummaryOnly(t *testing.T) {
	originalCollect, originalQuiet, originalOut := collect, quiet, outPath
	originalFormat, originalAll, originalProjects := config.Cfg.OutputFormat, allWorkspaces, onlyProjects
	originalWorkspace := config.Cfg.DefaultWorkspace
	originalDateField := dateField
	t.Cleanup(func() {
		collect, quiet, outPath = originalCollect, originalQuiet, originalOut
		config.Cfg.OutputFormat, allWorkspaces, onlyProjects = originalFormat, originalAll, originalProjects
		config.Cfg.DefaultWorkspace = originalWorkspace
		dateField = originalDateField
	})

	dir := t.TempDir()
	outPath = filepath.Join(dir, "snapshot.json")
	quiet = false
	config.Cfg.OutputFormat = "json"
	config.Cfg.DefaultWorkspace = "ws"
	collect = func(scope snap.Scope) (*snap.Snapshot, error) {
		assert.Equal(t, "ws", scope.Workspace)
		assert.Equal(t, []string{"GAME"}, scope.Projects)
		return &snap.Snapshot{
			StartedAt: "start", FinishedAt: "end",
			Coverage: []snap.ProjectCoverage{{Workspace: "ws", ProjectID: "p1", Items: 1, Retained: 1, Pages: 1}},
			Records:  []snap.Record{{Workspace: "ws", ProjectID: "p1"}},
		}, nil
	}
	allWorkspaces, onlyProjects, dateField = false, []string{"GAME"}, "updated"

	var out bytes.Buffer
	cmd := &cobra.Command{Use: "snapshot"}
	cmd.SetOut(&out)

	require.NoError(t, runSnapshot(cmd, nil))

	assert.FileExists(t, outPath)
	assert.Contains(t, out.String(), `"unique_items": 1`)
	assert.Contains(t, out.String(), `"file"`)
	assert.NotContains(t, out.String(), `"records"`)
	assert.True(t, strings.HasPrefix(strings.TrimSpace(out.String()), "{"))
}

func TestRunSnapshotYAMLSummaryUsesSnakeCaseKeys(t *testing.T) {
	originalCollect, originalQuiet, originalOut := collect, quiet, outPath
	originalFormat, originalAll, originalProjects := config.Cfg.OutputFormat, allWorkspaces, onlyProjects
	originalWorkspace := config.Cfg.DefaultWorkspace
	originalDateField := dateField
	t.Cleanup(func() {
		collect, quiet, outPath = originalCollect, originalQuiet, originalOut
		config.Cfg.OutputFormat, allWorkspaces, onlyProjects = originalFormat, originalAll, originalProjects
		config.Cfg.DefaultWorkspace = originalWorkspace
		dateField = originalDateField
	})

	dir := t.TempDir()
	outPath = filepath.Join(dir, "snapshot.json")
	quiet = false
	config.Cfg.OutputFormat = "yaml"
	config.Cfg.DefaultWorkspace = "ws"
	collect = func(scope snap.Scope) (*snap.Snapshot, error) {
		return &snap.Snapshot{
			StartedAt: "start", FinishedAt: "end",
			Coverage: []snap.ProjectCoverage{{Workspace: "ws", ProjectID: "p1", Items: 1, Retained: 1, Pages: 1}},
			Records:  []snap.Record{{Workspace: "ws", ProjectID: "p1"}},
		}, nil
	}
	allWorkspaces, onlyProjects, dateField = false, []string{"GAME"}, "updated"

	var out bytes.Buffer
	cmd := &cobra.Command{Use: "snapshot"}
	cmd.SetOut(&out)

	require.NoError(t, runSnapshot(cmd, nil))

	assert.Contains(t, out.String(), "started_at:")
	assert.Contains(t, out.String(), "unique_items:")
	assert.Contains(t, out.String(), "project_id:")
	assert.NotContains(t, out.String(), "startedat")
	assert.NotContains(t, out.String(), "uniqueitems")
	assert.NotContains(t, out.String(), "projectid")
	assert.NotContains(t, out.String(), "records")
}

func TestRunSnapshotAllWorkspacesIgnoresDefaultProject(t *testing.T) {
	originalCollect, originalQuiet, originalOut := collect, quiet, outPath
	originalFormat, originalAll, originalProjects := config.Cfg.OutputFormat, allWorkspaces, onlyProjects
	originalWorkspace := config.Cfg.DefaultWorkspace
	originalDefaultProject := config.Cfg.DefaultProject
	originalDateField := dateField
	t.Cleanup(func() {
		collect, quiet, outPath = originalCollect, originalQuiet, originalOut
		config.Cfg.OutputFormat, allWorkspaces, onlyProjects = originalFormat, originalAll, originalProjects
		config.Cfg.DefaultWorkspace = originalWorkspace
		config.Cfg.DefaultProject = originalDefaultProject
		dateField = originalDateField
	})

	dir := t.TempDir()
	outPath = filepath.Join(dir, "snapshot.json")
	quiet = true
	config.Cfg.OutputFormat = "json"
	config.Cfg.DefaultWorkspace = "ws"
	config.Cfg.DefaultProject = "SOME-PROJECT"
	collect = func(scope snap.Scope) (*snap.Snapshot, error) {
		assert.True(t, scope.AllWorkspaces)
		assert.Equal(t, "", scope.Workspace)
		assert.Len(t, scope.Projects, 0)
		return &snap.Snapshot{
			StartedAt: "start", FinishedAt: "end",
			Coverage: []snap.ProjectCoverage{}, Records: []snap.Record{},
		}, nil
	}
	allWorkspaces, onlyProjects, dateField = true, nil, "updated"

	cmd := &cobra.Command{Use: "snapshot"}
	require.NoError(t, runSnapshot(cmd, nil))
}

func TestRunSnapshotCollectFailureWritesNoFile(t *testing.T) {
	originalCollect, originalQuiet, originalOut := collect, quiet, outPath
	originalFormat, originalAll, originalProjects := config.Cfg.OutputFormat, allWorkspaces, onlyProjects
	originalDateField := dateField
	t.Cleanup(func() {
		collect, quiet, outPath = originalCollect, originalQuiet, originalOut
		config.Cfg.OutputFormat, allWorkspaces, onlyProjects = originalFormat, originalAll, originalProjects
		dateField = originalDateField
	})

	dir := t.TempDir()
	outPath = filepath.Join(dir, "snapshot.json")
	quiet = false
	config.Cfg.OutputFormat = "json"
	collect = func(scope snap.Scope) (*snap.Snapshot, error) {
		return nil, errors.New("collection failed")
	}
	allWorkspaces, onlyProjects, dateField = false, []string{"GAME"}, "updated"

	var out bytes.Buffer
	cmd := &cobra.Command{Use: "snapshot"}
	cmd.SetOut(&out)

	err := runSnapshot(cmd, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "collection failed")
	assert.NoFileExists(t, outPath)
	assert.Empty(t, out.String())
}
