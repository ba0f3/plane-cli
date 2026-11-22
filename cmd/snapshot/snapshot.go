package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/ba0f3/plane-cli/internal/output"
	snap "github.com/ba0f3/plane-cli/internal/snapshot"
	"github.com/spf13/cobra"
)

var (
	allWorkspaces bool
	onlyProjects  []string
	expand        string
	outPath       string
	since         string
	until         string
	dateField     string
	quiet         bool
)

// collect is a variable so command tests can substitute a stub without an API.
var collect = snap.Collect

var SnapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Collect a complete work-item snapshot in one invocation",
	Long: `Collect every selected work item into one local JSON snapshot.

The command paginates the work-items endpoint once per project, preserves the
server's raw fields, writes the bulk data to --out, and prints only a small
coverage summary to stdout. Use one snapshot for a report and calculate locally
instead of calling issue view for each task.

Scope rules:
  --all-workspaces is rejected when a workspace is already selected, whether it
  comes from --workspace, PLANE_WORKSPACE, or default_workspace in the config
  file. Clear it (env -u PLANE_WORKSPACE, or unset default_workspace) or drop
  --all-workspaces.

  The configured default project is not inherited: omitting --project collects
  every project in scope. Pass --project (or --only-project) to narrow the
  snapshot to specific projects.

If the collection fails, no new snapshot is written and any pre-existing --out
file is left untouched, so check the exit status before trusting that file.`,
	Args: cobra.NoArgs,
	RunE: runSnapshot,
}

func init() {
	SnapshotCmd.Flags().BoolVar(&allWorkspaces, "all-workspaces", false, "Collect every visible workspace (IAT); rejected when a workspace is selected by flag, env, or config")
	SnapshotCmd.Flags().StringArrayVar(&onlyProjects, "only-project", nil, "Project ID, identifier, or name (repeatable)")
	SnapshotCmd.Flags().StringVar(&expand, "expand", "assignees,state,labels", "Comma-separated work-item expansions")
	SnapshotCmd.Flags().StringVar(&outPath, "out", "./plane-snapshot.json", "Path for the bulk snapshot JSON")
	SnapshotCmd.Flags().StringVar(&since, "since", "", "Start of local filter: RFC3339, YYYY-MM-DD, duration, or Nd")
	SnapshotCmd.Flags().StringVar(&until, "until", "", "End of local filter: RFC3339 or YYYY-MM-DD")
	SnapshotCmd.Flags().StringVar(&dateField, "date-field", "updated", "Field for the local filter: created or updated")
	SnapshotCmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress the stdout summary")
}

func validate(cmd *cobra.Command) error {
	// The root PreRun copies --workspace into config.Cfg.DefaultWorkspace, so a
	// configured workspace is just as much a selection as an explicit flag.
	if allWorkspaces && (cmd.InheritedFlags().Changed("workspace") ||
		strings.TrimSpace(config.Cfg.DefaultWorkspace) != "") {
		return fmt.Errorf("a workspace is already selected (--workspace or config); unset it or drop --all-workspaces")
	}

	// Normalize once so the command and the collector share the same shape.
	raw := dateField
	normalized := strings.ToLower(strings.TrimSpace(dateField))
	if normalized != "" && normalized != "created" && normalized != "updated" {
		return fmt.Errorf("invalid date field %q: use created or updated", raw)
	}
	dateField = normalized
	return nil
}

func runSnapshot(cmd *cobra.Command, args []string) error {
	if err := validate(cmd); err != nil {
		return err
	}

	scope := snap.Scope{
		Expand: expand, Since: since, Until: until, DateField: dateField,
		Projects: onlyProjects, AllWorkspaces: allWorkspaces,
	}
	if allWorkspaces {
		scope.Workspace = ""
	} else {
		scope.Workspace = config.Cfg.DefaultWorkspace
	}
	// The configured default project must never silently narrow a snapshot:
	// only an explicit global --project is honoured, so omitting it means
	// "every project in scope".
	if cmd.InheritedFlags().Changed("project") && len(scope.Projects) == 0 &&
		strings.TrimSpace(config.Cfg.DefaultProject) != "" {
		scope.Projects = []string{config.Cfg.DefaultProject}
	}

	result, err := collect(scope)
	if err != nil {
		return err
	}
	if err := writeSnapshot(outPath, result); err != nil {
		return err
	}
	if quiet {
		return nil
	}
	return printSummary(cmd, outPath, result)
}

func writeSnapshot(path string, snapshot *snap.Snapshot) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".plane-snapshot-*.json")
	if err != nil {
		return fmt.Errorf("create temporary snapshot: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write snapshot: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close snapshot: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install snapshot: %w", err)
	}
	return nil
}

func printSummary(cmd *cobra.Command, path string, snapshot *snap.Snapshot) error {
	summary := snap.Summary{
		File: path, StartedAt: snapshot.StartedAt, FinishedAt: snapshot.FinishedAt,
		UniqueItems: len(snapshot.Records), Coverage: snapshot.Coverage,
	}

	return output.NewFormatter(config.Cfg.OutputFormat, false).Fprint(cmd.OutOrStdout(), summary)
}
