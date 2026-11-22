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

	originalWorkspace := config.Cfg.DefaultWorkspace
	t.Cleanup(func() {
		config.Cfg.DefaultWorkspace = originalWorkspace
		allWorkspaces = false
	})
	config.Cfg.DefaultWorkspace = ""

	allWorkspaces = true

	err := validate(sub)
	assert.ErrorContains(t, err, "--all-workspaces")
	assert.ErrorContains(t, err, "workspace is already selected")
}

func TestValidateRejectsAllWorkspacesWithConfiguredWorkspace(t *testing.T) {
	root := &cobra.Command{Use: "plane-cli"}
	root.PersistentFlags().String("workspace", "", "")
	sub := &cobra.Command{Use: "snapshot"}
	root.AddCommand(sub)

	originalWorkspace := config.Cfg.DefaultWorkspace
	t.Cleanup(func() {
		config.Cfg.DefaultWorkspace = originalWorkspace
		allWorkspaces = false
	})
	// A workspace selected through config is just as much a selection as an
	// explicit --workspace flag.
	config.Cfg.DefaultWorkspace = "ws"
	allWorkspaces = true

	err := validate(sub)
	assert.ErrorContains(t, err, "workspace is already selected")
}

func TestValidateAcceptsAllWorkspacesWithoutWorkspace(t *testing.T) {
	root := &cobra.Command{Use: "plane-cli"}
	root.PersistentFlags().String("workspace", "", "")
	sub := &cobra.Command{Use: "snapshot"}
	root.AddCommand(sub)

	originalWorkspace := config.Cfg.DefaultWorkspace
	t.Cleanup(func() {
		config.Cfg.DefaultWorkspace = originalWorkspace
		allWorkspaces = false
		dateField = "updated"
	})
	config.Cfg.DefaultWorkspace = ""
	allWorkspaces = true
	dateField = "updated"

	require.NoError(t, validate(sub))
}

func TestValidateRejectsInvalidDateField(t *testing.T) {
	// A mixed-case value is used on purpose: the normalization lowercases the
	// value, so only a mixed-case input can detect the global being mutated
	// before the rejection.
	dateField = "COMPLETED"
	t.Cleanup(func() { dateField = "updated" })

	err := validate(&cobra.Command{Use: "snapshot"})
	assert.ErrorContains(t, err, "date field")
	// The rejected value must not be normalized into the global.
	assert.Equal(t, "COMPLETED", dateField)
}

func TestSnapshotHelpDocumentsScopeRules(t *testing.T) {
	// The flag help and the long description must state the scope rules that
	// validate enforces.
	flagHelp := SnapshotCmd.Flags().Lookup("all-workspaces").Usage
	assert.Contains(t, flagHelp, "rejected when a workspace is selected by flag, env, or config")

	assert.Contains(t, SnapshotCmd.Long, "--all-workspaces is rejected when a workspace is already selected")
	assert.Contains(t, SnapshotCmd.Long, "PLANE_WORKSPACE")
	assert.Contains(t, SnapshotCmd.Long, "default_workspace")
	assert.Contains(t, SnapshotCmd.Long, "env -u PLANE_WORKSPACE")
	assert.Contains(t, SnapshotCmd.Long, "default project is not inherited")
	assert.Contains(t, SnapshotCmd.Long, "--project")
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

func TestRunSnapshotAllWorkspacesHasNoProjectFallback(t *testing.T) {
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
	// --all-workspaces requires that no workspace is selected.
	config.Cfg.DefaultWorkspace = ""
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

func TestRunSnapshotIgnoresConfiguredDefaultProject(t *testing.T) {
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
	// A configured default project must not silently narrow the snapshot.
	config.Cfg.DefaultProject = "SOME-PROJECT"
	collect = func(scope snap.Scope) (*snap.Snapshot, error) {
		assert.Len(t, scope.Projects, 0)
		return &snap.Snapshot{
			StartedAt: "start", FinishedAt: "end",
			Coverage: []snap.ProjectCoverage{}, Records: []snap.Record{},
		}, nil
	}
	allWorkspaces, onlyProjects, dateField = false, nil, "updated"

	cmd := &cobra.Command{Use: "snapshot"}
	require.NoError(t, runSnapshot(cmd, nil))
}

func TestRunSnapshotUsesExplicitProjectFlag(t *testing.T) {
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
	config.Cfg.DefaultProject = "EXPLICIT"
	collect = func(scope snap.Scope) (*snap.Snapshot, error) {
		assert.Equal(t, []string{"EXPLICIT"}, scope.Projects)
		return &snap.Snapshot{
			StartedAt: "start", FinishedAt: "end",
			Coverage: []snap.ProjectCoverage{}, Records: []snap.Record{},
		}, nil
	}
	allWorkspaces, onlyProjects, dateField = false, nil, "updated"

	root := &cobra.Command{Use: "plane-cli"}
	root.PersistentFlags().String("project", "", "")
	sub := &cobra.Command{Use: "snapshot"}
	root.AddCommand(sub)
	require.NoError(t, root.PersistentFlags().Set("project", "EXPLICIT"))

	require.NoError(t, runSnapshot(sub, nil))
}

func TestRunSnapshotCollectFailureKeepsExistingOutFile(t *testing.T) {
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
	require.NoError(t, os.WriteFile(outPath, []byte("{\"previous\":true}\n"), 0o600))

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

	// The pre-existing file must be left untouched.
	data, readErr := os.ReadFile(outPath)
	require.NoError(t, readErr)
	assert.Equal(t, "{\"previous\":true}\n", string(data))
	assert.Empty(t, out.String())
}

func TestRunSnapshotQuietProducesEmptyStdout(t *testing.T) {
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
	quiet = true
	config.Cfg.OutputFormat = "json"
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

	assert.FileExists(t, outPath)
	assert.Empty(t, out.String())
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
