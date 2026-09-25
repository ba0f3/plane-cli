package workspace

import (
	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/ba0f3/plane-cli/internal/output"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List workspaces visible to the current PAT/WSAT/IAT",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := api.NewClientNoWorkspace()
		if err != nil {
			return err
		}
		workspaces, err := client.ListWorkspaces()
		if err != nil {
			return err
		}

		type workspaceOutput struct {
			ID      string `table:"ID" json:"id"`
			Slug    string `table:"SLUG" json:"slug"`
			Name    string `table:"NAME" json:"name"`
			Default string `table:"DEFAULT" json:"default,omitempty"`
		}
		rows := make([]workspaceOutput, 0, len(workspaces))
		for _, ws := range workspaces {
			def := ""
			if ws.Slug == config.Cfg.DefaultWorkspace {
				def = "✓"
			}
			rows = append(rows, workspaceOutput{ID: ws.ID, Slug: ws.Slug, Name: ws.Name, Default: def})
		}
		return output.NewFormatter(config.Cfg.OutputFormat, false).Print(rows)
	},
}

func init() {
	WorkspaceCmd.AddCommand(listCmd)
}
