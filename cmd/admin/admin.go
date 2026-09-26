package admin

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ba0f3/plane-cli/internal/adminops"
	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/ba0f3/plane-cli/internal/output"
	"github.com/ba0f3/plane-cli/pkg/plane"
	"github.com/spf13/cobra"
)

var (
	memberSearch string
	bulkApply    bool
)

var AdminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Instance administration, audit, and safe bulk operations",
}

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Diagnose the current token and admin-read readiness",
	Args:  cobra.NoArgs,
	RunE:  runToken,
}

var membersCmd = &cobra.Command{
	Use:   "members [workspace]",
	Short: "Inventory members across visible workspaces",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runMembers,
}

var healthCmd = &cobra.Command{
	Use:   "health [workspace]",
	Short: "Check workspace/project API health and inventory counts",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runHealth,
}

var auditCmd = &cobra.Command{
	Use:   "audit [workspace]",
	Short: "Audit visible workspace/project membership consistency",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runAudit,
}

var bulkCmd = &cobra.Command{
	Use:   "bulk",
	Short: "Safe bulk administration commands",
}

var bulkProjectDeleteCmd = &cobra.Command{
	Use:   "project-delete <workspace> <project-selector> [project-selector...]",
	Short: "Plan or apply deletion of multiple projects in one explicit workspace",
	Long: `Delete multiple projects by ID, identifier, or exact name.

This command is dry-run by default. It never takes the workspace implicitly from
config or environment. Pass --apply to execute the planned deletions.

Examples:
  plane-cli admin bulk project-delete engineering OLD1 OLD2
  plane-cli admin bulk project-delete engineering OLD1 OLD2 --apply`,
	Args: cobra.MinimumNArgs(2),
	RunE: runBulkProjectDelete,
}

func init() {
	membersCmd.Flags().StringVar(&memberSearch, "search", "", "Filter inventory by id, email, or name")
	bulkProjectDeleteCmd.Flags().BoolVar(&bulkApply, "apply", false, "Execute deletions; without this flag the command is dry-run only")

	bulkCmd.AddCommand(bulkProjectDeleteCmd)
	AdminCmd.AddCommand(tokenCmd, membersCmd, healthCmd, auditCmd, bulkCmd)
}

type check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type tokenReport struct {
	PrincipalType            string   `json:"principal_type,omitempty"`
	ScopeLevel               string   `json:"scope_level,omitempty"`
	IsService                bool     `json:"is_service"`
	BoundWorkspace           string   `json:"bound_workspace,omitempty"`
	Scopes                   []string `json:"scopes,omitempty"`
	VisibleWorkspaces        int      `json:"visible_workspaces"`
	MissingRecommendedScopes []string `json:"missing_recommended_scopes,omitempty"`
	BulkProjectDeleteReady   bool     `json:"bulk_project_delete_ready"`
	Checks                   []check  `json:"checks"`
}

type scanError struct {
	Workspace string `json:"workspace"`
	Operation string `json:"operation"`
	Error     string `json:"error"`
}

type memberReport struct {
	WorkspacesScanned int                        `json:"workspaces_scanned"`
	Members           []adminops.MemberInventory `json:"members"`
	Errors            []scanError                `json:"errors"`
}

type projectHealth struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Members int    `json:"members"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

type workspaceHealth struct {
	Workspace string          `json:"workspace"`
	Name      string          `json:"name,omitempty"`
	Members   int             `json:"members"`
	Projects  int             `json:"projects"`
	Status    string          `json:"status"`
	Errors    []scanError     `json:"errors"`
	Project   []projectHealth `json:"project_health"`
}

type healthSummary struct {
	Workspaces int `json:"workspaces"`
	Healthy    int `json:"healthy"`
	Degraded   int `json:"degraded"`
	Projects   int `json:"projects"`
	Errors     int `json:"errors"`
}

type healthReport struct {
	Status     string            `json:"status"`
	Summary    healthSummary     `json:"summary"`
	Workspaces []workspaceHealth `json:"workspaces"`
}

type deleteTarget struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Name       string `json:"name"`
}

type deleteResult struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

type bulkDeleteReport struct {
	Mode      string         `json:"mode"`
	Workspace string         `json:"workspace"`
	Targets   []deleteTarget `json:"targets"`
	Missing   []string       `json:"missing,omitempty"`
	Results   []deleteResult `json:"results,omitempty"`
}

func runToken(cmd *cobra.Command, args []string) error {
	client, err := api.NewClientNoWorkspace()
	if err != nil {
		return err
	}
	report := tokenReport{Checks: []check{}, Scopes: []string{}}

	ctx, ctxErr := client.GetAuthContext()
	if ctxErr != nil {
		report.Checks = append(report.Checks, check{Name: "auth_context", Status: "warning", Message: ctxErr.Error()})
		if _, userErr := client.GetUserInfo(); userErr == nil {
			report.PrincipalType = "user"
			report.ScopeLevel = "user"
			report.Checks = append(report.Checks, check{Name: "user_identity", Status: "ok", Message: "authenticated user endpoint is reachable"})
		} else {
			report.Checks = append(report.Checks, check{Name: "user_identity", Status: "error", Message: userErr.Error()})
		}
	} else {
		report.PrincipalType = ctx.PrincipalType
		report.ScopeLevel = ctx.ScopeLevel
		report.IsService = ctx.IsService
		report.Scopes = append(report.Scopes, ctx.Scopes...)
		sort.Strings(report.Scopes)
		if ctx.Workspace != nil {
			report.BoundWorkspace = ctx.Workspace.Slug
		}
		report.Checks = append(report.Checks, check{Name: "auth_context", Status: "ok", Message: "service-token discovery endpoint is reachable"})
	}

	workspaces, workspaceErr := client.ListWorkspaces()
	if workspaceErr != nil {
		report.Checks = append(report.Checks, check{Name: "workspace_discovery", Status: "error", Message: workspaceErr.Error()})
	} else {
		report.VisibleWorkspaces = len(workspaces)
		report.Checks = append(report.Checks, check{Name: "workspace_discovery", Status: "ok", Message: fmt.Sprintf("%d workspace(s) visible", len(workspaces))})
	}

	if report.IsService {
		recommended := []string{"projects:read", "workspaces.members:read", "workspaces:read"}
		for _, scope := range recommended {
			if !hasScope(report.Scopes, scope) {
				report.MissingRecommendedScopes = append(report.MissingRecommendedScopes, scope)
			}
		}
		report.BulkProjectDeleteReady = hasScope(report.Scopes, "projects:write")
		status := "ok"
		message := "recommended admin read scopes are present"
		if len(report.MissingRecommendedScopes) > 0 {
			status = "warning"
			message = "one or more recommended admin read scopes are missing"
		}
		report.Checks = append(report.Checks, check{Name: "admin_read_scopes", Status: status, Message: message})
	} else {
		report.BulkProjectDeleteReady = report.PrincipalType == "user"
		report.Checks = append(report.Checks, check{Name: "admin_read_scopes", Status: "not_applicable", Message: "PAT/user permissions are enforced by Plane membership and role"})
	}

	return print(report)
}

func runMembers(cmd *cobra.Command, args []string) error {
	client, workspaces, err := resolveWorkspaces(args)
	if err != nil {
		return err
	}
	byWorkspace := map[string][]plane.User{}
	errors := []scanError{}
	for _, workspace := range workspaces {
		members, memberErr := client.CloneForWorkspace(workspace.Slug).GetWorkspaceMembers()
		if memberErr != nil {
			errors = append(errors, scanError{Workspace: workspace.Slug, Operation: "members", Error: memberErr.Error()})
			continue
		}
		byWorkspace[workspace.Slug] = members
	}
	report := memberReport{
		WorkspacesScanned: len(workspaces),
		Members:           adminops.BuildMemberInventory(byWorkspace, memberSearch),
		Errors:            errors,
	}
	return print(report)
}

func runHealth(cmd *cobra.Command, args []string) error {
	client, workspaces, err := resolveWorkspaces(args)
	if err != nil {
		return err
	}

	report := healthReport{Status: "healthy", Workspaces: []workspaceHealth{}}
	for _, workspace := range workspaces {
		item := workspaceHealth{Workspace: workspace.Slug, Name: workspace.Name, Status: "healthy", Errors: []scanError{}, Project: []projectHealth{}}
		scoped := client.CloneForWorkspace(workspace.Slug)
		members, memberErr := scoped.GetWorkspaceMembers()
		if memberErr != nil {
			item.Status = "degraded"
			item.Errors = append(item.Errors, scanError{Workspace: workspace.Slug, Operation: "members", Error: memberErr.Error()})
		} else {
			item.Members = len(members)
		}
		projects, projectErr := scoped.ListProjects()
		if projectErr != nil {
			item.Status = "degraded"
			item.Errors = append(item.Errors, scanError{Workspace: workspace.Slug, Operation: "projects", Error: projectErr.Error()})
		} else {
			item.Projects = len(projects)
			for _, project := range projects {
				projectItem := projectHealth{ID: project.ID, Project: project.Identifier, Status: "healthy"}
				projectMembers, projectMemberErr := scoped.GetProjectMembers(project.ID)
				if projectMemberErr != nil {
					projectItem.Status = "degraded"
					projectItem.Error = projectMemberErr.Error()
					item.Status = "degraded"
				} else {
					projectItem.Members = len(projectMembers)
				}
				item.Project = append(item.Project, projectItem)
			}
		}
		report.Workspaces = append(report.Workspaces, item)
	}

	report.Summary.Workspaces = len(report.Workspaces)
	for _, workspace := range report.Workspaces {
		report.Summary.Projects += workspace.Projects
		report.Summary.Errors += len(workspace.Errors)
		for _, project := range workspace.Project {
			if project.Error != "" {
				report.Summary.Errors++
			}
		}
		if workspace.Status == "healthy" {
			report.Summary.Healthy++
		} else {
			report.Summary.Degraded++
			report.Status = "degraded"
		}
	}
	return print(report)
}

func runAudit(cmd *cobra.Command, args []string) error {
	client, workspaces, err := resolveWorkspaces(args)
	if err != nil {
		return err
	}
	snapshots := make([]adminops.WorkspaceSnapshot, 0, len(workspaces))
	for _, workspace := range workspaces {
		snapshot := adminops.WorkspaceSnapshot{Workspace: workspace.Slug, Projects: []adminops.ProjectSnapshot{}}
		scoped := client.CloneForWorkspace(workspace.Slug)
		members, memberErr := scoped.GetWorkspaceMembers()
		if memberErr != nil {
			snapshot.Error = "list workspace members: " + memberErr.Error()
			snapshots = append(snapshots, snapshot)
			continue
		}
		snapshot.Members = members
		projects, projectErr := scoped.ListProjects()
		if projectErr != nil {
			snapshot.Error = "list projects: " + projectErr.Error()
			snapshots = append(snapshots, snapshot)
			continue
		}
		for _, project := range projects {
			projectSnapshot := adminops.ProjectSnapshot{Project: project}
			projectMembers, projectMemberErr := scoped.GetProjectMembers(project.ID)
			if projectMemberErr != nil {
				projectSnapshot.Error = projectMemberErr.Error()
			} else {
				projectSnapshot.Members = projectMembers
			}
			snapshot.Projects = append(snapshot.Projects, projectSnapshot)
		}
		snapshots = append(snapshots, snapshot)
	}
	return print(adminops.Audit(snapshots))
}

func runBulkProjectDelete(cmd *cobra.Command, args []string) error {
	workspace := strings.TrimSpace(args[0])
	selectors := args[1:]
	client, err := api.NewClientNoWorkspace()
	if err != nil {
		return err
	}
	scoped := client.CloneForWorkspace(workspace)
	projects, err := scoped.ListProjects()
	if err != nil {
		return fmt.Errorf("workspace %s: list projects: %w", workspace, err)
	}
	plan := adminops.ResolveProjects(projects, selectors)
	report := bulkDeleteReport{Mode: "dry-run", Workspace: workspace, Missing: plan.Missing, Targets: []deleteTarget{}}
	for _, project := range plan.Projects {
		report.Targets = append(report.Targets, deleteTarget{ID: project.ID, Identifier: project.Identifier, Name: project.Name})
	}
	if !bulkApply {
		return print(report)
	}
	if len(plan.Missing) > 0 {
		if err := print(report); err != nil {
			return err
		}
		return fmt.Errorf("refusing to apply: %d project selector(s) did not match", len(plan.Missing))
	}
	if len(plan.Projects) == 0 {
		return fmt.Errorf("refusing to apply: no projects matched")
	}

	report.Mode = "applied"
	failures := 0
	for _, project := range plan.Projects {
		result := deleteResult{ID: project.ID, Identifier: project.Identifier, Status: "deleted"}
		if deleteErr := scoped.DeleteProject(project.ID); deleteErr != nil {
			result.Status = "failed"
			result.Error = deleteErr.Error()
			failures++
		}
		report.Results = append(report.Results, result)
	}
	if err := print(report); err != nil {
		return err
	}
	if failures > 0 {
		return fmt.Errorf("%d project deletion(s) failed", failures)
	}
	return nil
}

func resolveWorkspaces(args []string) (*api.Client, []api.WorkspaceSummary, error) {
	client, err := api.NewClientNoWorkspace()
	if err != nil {
		return nil, nil, err
	}
	if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
		workspace, workspaceErr := client.GetWorkspace(args[0])
		if workspaceErr != nil {
			return nil, nil, workspaceErr
		}
		return client, []api.WorkspaceSummary{*workspace}, nil
	}
	workspaces, err := client.ListWorkspaces()
	if err != nil {
		return nil, nil, err
	}
	return client, workspaces, nil
}

func hasScope(scopes []string, expected string) bool {
	for _, scope := range scopes {
		if scope == expected || scope == "*" {
			return true
		}
	}
	return false
}

func print(value interface{}) error {
	return output.NewFormatter(config.Cfg.OutputFormat, false).Print(value)
}
