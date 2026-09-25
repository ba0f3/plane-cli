package workspace

import (
	"fmt"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/rohithmahesh3/plane-cli/internal/api"
	"github.com/rohithmahesh3/plane-cli/internal/config"
	"github.com/rohithmahesh3/plane-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	memberSearch string
	memberExact  bool
	memberLimit  int
)

var WorkspaceCmd = &cobra.Command{
	Use:     "workspace",
	Aliases: []string{"ws"},
	Short:   "Discover and manage Plane workspaces",
	Long:    "Discover workspaces visible to PAT, WSAT, or IAT credentials and select a default workspace.",
}

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List workspaces visible to the current credential",
	RunE:    runList,
}

var infoCmd = &cobra.Command{
	Use:   "info [slug]",
	Short: "Show workspace details",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runInfo,
}

var switchCmd = &cobra.Command{
	Use:   "switch [slug]",
	Short: "Switch default workspace",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runSwitch,
}

var membersCmd = &cobra.Command{
	Use:     "members",
	Aliases: []string{"users", "people"},
	Short:   "List workspace members",
	RunE:    runMembers,
}

func init() {
	membersCmd.Flags().StringVar(&memberSearch, "search", "", "Filter members by display name, email, full name, or ID")
	membersCmd.Flags().BoolVar(&memberExact, "exact", false, "Require exact matches for --search")
	membersCmd.Flags().IntVar(&memberLimit, "limit", 0, "Maximum number of members to show (0 = no limit)")

	WorkspaceCmd.AddCommand(listCmd)
	WorkspaceCmd.AddCommand(infoCmd)
	WorkspaceCmd.AddCommand(switchCmd)
	WorkspaceCmd.AddCommand(membersCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	client, err := api.NewClientNoWorkspace()
	if err != nil {
		return err
	}
	workspaces, err := client.ListWorkspaces()
	if err != nil {
		return fmt.Errorf("workspace discovery failed: %w", err)
	}
	if len(workspaces) == 0 {
		output.Info("No workspaces visible to this credential")
		return nil
	}

	type row struct {
		ID      string `table:"ID" json:"id"`
		Slug    string `table:"SLUG" json:"slug"`
		Name    string `table:"NAME" json:"name"`
		Default string `table:"DEFAULT" json:"default,omitempty"`
	}
	rows := make([]row, 0, len(workspaces))
	for _, ws := range workspaces {
		def := ""
		if ws.Slug == config.Cfg.DefaultWorkspace {
			def = "✓"
		}
		rows = append(rows, row{ID: ws.ID, Slug: ws.Slug, Name: ws.Name, Default: def})
	}
	return output.NewFormatter(config.Cfg.OutputFormat, false).Print(rows)
}

func runInfo(cmd *cobra.Command, args []string) error {
	slug := config.Cfg.DefaultWorkspace
	if len(args) > 0 {
		slug = args[0]
	}

	client, err := api.NewClientNoWorkspace()
	if err != nil {
		return err
	}
	if slug == "" {
		ctx, err := client.GetAuthContext()
		if err == nil && ctx.Workspace != nil {
			slug = ctx.Workspace.Slug
		}
	}
	if slug == "" {
		return fmt.Errorf("no workspace selected; use 'plane-cli workspace list' or pass a slug")
	}

	ws, err := client.GetWorkspace(slug)
	if err != nil {
		return err
	}
	return output.NewFormatter(config.Cfg.OutputFormat, false).Print(ws)
}

func runSwitch(cmd *cobra.Command, args []string) error {
	var slug string
	if len(args) > 0 {
		slug = args[0]
	} else {
		client, err := api.NewClientNoWorkspace()
		if err != nil {
			return err
		}
		workspaces, err := client.ListWorkspaces()
		if err == nil && len(workspaces) > 0 {
			options := make([]string, len(workspaces))
			byOption := map[string]string{}
			for i, ws := range workspaces {
				label := fmt.Sprintf("%s (%s)", ws.Name, ws.Slug)
				options[i] = label
				byOption[label] = ws.Slug
			}
			var selected string
			if err := survey.AskOne(&survey.Select{Message: "Select workspace:", Options: options}, &selected); err != nil {
				return err
			}
			slug = byOption[selected]
		} else {
			if err := survey.AskOne(&survey.Input{Message: "Enter workspace slug:"}, &slug, survey.WithValidator(survey.Required)); err != nil {
				return err
			}
		}
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return fmt.Errorf("workspace slug is required")
	}

	client, err := api.NewClientNoWorkspace()
	if err != nil {
		return err
	}
	if _, err := client.GetWorkspace(slug); err != nil {
		// Compatibility fallback for upstream Plane without discovery.
		client.SetWorkspace(slug)
		if _, projectErr := client.ListProjects(); projectErr != nil {
			return fmt.Errorf("could not access workspace %q: discovery=%v; project validation=%w", slug, err, projectErr)
		}
	}

	config.Cfg.DefaultWorkspace = slug
	if err := config.SaveConfig(); err != nil {
		return err
	}
	output.Success(fmt.Sprintf("Switched to workspace '%s'", slug))
	return nil
}

func runMembers(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}
	members, err := client.GetWorkspaceMembers()
	if err != nil {
		return err
	}

	limit := memberLimit
	if memberSearch != "" && !cmd.Flags().Changed("limit") {
		limit = 20
	}
	members = api.FilterWorkspaceMembers(members, memberSearch, memberExact, limit)
	if len(members) == 0 {
		output.Info("No members found")
		return nil
	}

	formatter := output.NewFormatter(config.Cfg.OutputFormat, false)
	type memberOutput struct {
		ID          string `table:"ID" json:"id"`
		DisplayName string `table:"DISPLAY_NAME" json:"display_name"`
		Email       string `table:"EMAIL" json:"email"`
		FirstName   string `table:"FIRST_NAME" json:"first_name"`
		LastName    string `table:"LAST_NAME" json:"last_name"`
		Role        int    `table:"ROLE" json:"role"`
	}
	outputs := make([]memberOutput, 0, len(members))
	for _, m := range members {
		outputs = append(outputs, memberOutput{
			ID: m.ID, DisplayName: m.DisplayName, Email: m.Email,
			FirstName: m.FirstName, LastName: m.LastName, Role: m.Role,
		})
	}
	return formatter.Print(outputs)
}
