package digest

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
	semantic "github.com/ba0f3/plane-cli/internal/digest"
	"github.com/ba0f3/plane-cli/internal/output"
	"github.com/ba0f3/plane-cli/pkg/plane"
	"github.com/spf13/cobra"
)

var (
	digestSince     string
	digestStaleDays int
	digestLimit     int
	digestTimezone  string
)

var DigestCmd = &cobra.Command{
	Use:   "digest",
	Short: "Semantic work-item digests for users, projects, and workspaces",
	Long: `Build automation-friendly semantic digests from Plane work items.

Sections are independent and may overlap. For example, one work item may be both
blocked and overdue. Completed items use --since as the lookback window; active
sections always evaluate the current state.`,
}

var userCmd = &cobra.Command{
	Use:   "user <id|email|name>",
	Short: "Build a digest for one assignee across accessible workspaces",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDigest("user", args[0])
	},
}

var projectCmd = &cobra.Command{
	Use:   "project [id|identifier|name]",
	Short: "Build a digest for one project",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		selector := config.Cfg.DefaultProject
		if len(args) > 0 {
			selector = args[0]
		}
		if selector == "" {
			return fmt.Errorf("project selector is required; pass an argument or configure --project")
		}
		return runDigest("project", selector)
	},
}

var workspaceCmd = &cobra.Command{
	Use:   "workspace [slug]",
	Short: "Build a digest for one workspace",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		selector := config.Cfg.DefaultWorkspace
		if len(args) > 0 {
			selector = args[0]
		}
		if selector == "" {
			return fmt.Errorf("workspace selector is required; pass a slug or configure --workspace")
		}
		return runDigest("workspace", selector)
	},
}

func init() {
	DigestCmd.PersistentFlags().StringVar(&digestSince, "since", "24h", "Completed lookback: RFC3339, YYYY-MM-DD, Go duration (24h), or Nd (7d)")
	DigestCmd.PersistentFlags().IntVar(&digestStaleDays, "stale-days", 7, "Mark active work items stale after this many days without updates")
	DigestCmd.PersistentFlags().IntVarP(&digestLimit, "limit", "l", 100, "Maximum items per semantic section; 0 means unlimited")
	DigestCmd.PersistentFlags().StringVar(&digestTimezone, "timezone", "Local", "Timezone for due-today evaluation, e.g. Asia/Ho_Chi_Minh, UTC, or Local")
	DigestCmd.AddCommand(userCmd, projectCmd, workspaceCmd)
}

func runDigest(scope, selector string) error {
	if digestStaleDays <= 0 {
		return fmt.Errorf("--stale-days must be greater than zero")
	}
	if digestLimit < 0 {
		return fmt.Errorf("--limit must be >= 0")
	}

	location, err := parseLocation(digestTimezone)
	if err != nil {
		return err
	}
	now := time.Now().In(location)
	since, err := parseTime(digestSince, now)
	if err != nil {
		return fmt.Errorf("invalid --since: %w", err)
	}
	if since.After(now) {
		return fmt.Errorf("--since cannot be in the future")
	}

	records, err := collectRecords(scope, selector)
	if err != nil {
		return err
	}
	report, err := semantic.Build(records, semantic.Options{
		Scope:     scope,
		Selector:  selector,
		Since:     since,
		Now:       now,
		StaleDays: digestStaleDays,
		Limit:     digestLimit,
	})
	if err != nil {
		return err
	}
	return output.NewFormatter(config.Cfg.OutputFormat, false).Print(report)
}

func collectRecords(scope, selector string) ([]semantic.Record, error) {
	client, err := api.NewClientNoWorkspace()
	if err != nil {
		return nil, err
	}

	workspaceFilter := config.Cfg.DefaultWorkspace
	if scope == "workspace" {
		workspaceFilter = selector
	}

	var workspaces []api.WorkspaceSummary
	if workspaceFilter != "" {
		workspaces = []api.WorkspaceSummary{{Slug: workspaceFilter, Name: workspaceFilter}}
	} else {
		workspaces, err = client.ListWorkspaces()
		if err != nil {
			return nil, fmt.Errorf("list workspaces: %w", err)
		}
	}

	var records []semantic.Record
	for _, workspace := range workspaces {
		scoped := client.CloneForWorkspace(workspace.Slug)
		projects, err := scoped.ListProjects()
		if err != nil {
			return nil, fmt.Errorf("workspace %s: list projects: %w", workspace.Slug, err)
		}
		for _, project := range projects {
			if scope == "project" && !projectMatches(project, selector) {
				continue
			}
			issues, err := scoped.ListAllIssues(project.ID)
			if err != nil {
				return nil, fmt.Errorf("workspace %s project %s: list work items: %w", workspace.Slug, project.Identifier, err)
			}
			for _, issue := range issues {
				records = append(records, semantic.Record{
					Workspace: workspace.Slug,
					Project:   project,
					Issue:     issue,
				})
			}
		}
	}
	return records, nil
}

func projectMatches(project plane.Project, selector string) bool {
	selector = strings.TrimSpace(selector)
	return strings.EqualFold(project.ID, selector) ||
		strings.EqualFold(project.Identifier, selector) ||
		strings.EqualFold(project.Name, selector)
}

func parseLocation(value string) (*time.Location, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "local") {
		return time.Local, nil
	}
	location, err := time.LoadLocation(value)
	if err != nil {
		return nil, fmt.Errorf("invalid --timezone %q: %w", value, err)
	}
	return location, nil
}

func parseTime(value string, now time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err == nil && days >= 0 {
			return now.AddDate(0, 0, -days), nil
		}
	}
	if duration, err := time.ParseDuration(value); err == nil {
		return now.Add(-duration), nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.In(now.Location()), nil
	}
	if parsed, err := time.ParseInLocation("2006-01-02", value, now.Location()); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q", value)
}
