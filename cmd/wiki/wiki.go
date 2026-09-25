package wiki

import (
	"fmt"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/ba0f3/plane-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	archived     bool
	updatedAfter string
	pageName     string
	pageHTML     string
	pageParent   string
	pageColor    string
	pageSort     float64
)

var WikiCmd = &cobra.Command{
	Use:   "wiki",
	Short: "Manage fork Wiki pages through the public service-token API",
	Long:  "Read Wiki pages with eligible credentials and mutate them with a service token carrying wiki.pages:write.",
}

func init() {
	list := &cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List Wiki pages", RunE: runList}
	show := &cobra.Command{Use: "show <page-id>", Short: "Show one Wiki page", Args: cobra.ExactArgs(1), RunE: runShow}
	create := &cobra.Command{Use: "create", Short: "Create a Wiki page", RunE: runCreate}
	update := &cobra.Command{Use: "update <page-id>", Short: "Update a Wiki page", Args: cobra.ExactArgs(1), RunE: runUpdate}

	list.Flags().BoolVar(&archived, "archived", false, "List archived pages")
	list.Flags().StringVar(&updatedAfter, "updated-after", "", "Only pages updated after ISO-8601 timestamp")

	for _, c := range []*cobra.Command{create, update} {
		c.Flags().StringVar(&pageName, "name", "", "Page name")
		c.Flags().StringVar(&pageHTML, "html", "", "Page description_html")
		c.Flags().StringVar(&pageParent, "parent", "", "Parent page UUID")
		c.Flags().StringVar(&pageColor, "color", "", "Page color")
		c.Flags().Float64Var(&pageSort, "sort-order", 0, "Page sort order")
	}

	WikiCmd.AddCommand(list, show, create, update)
	for _, action := range []string{"archive", "unarchive", "lock", "unlock"} {
		a := action
		WikiCmd.AddCommand(&cobra.Command{
			Use:   a + " <page-id>",
			Short: a + " a Wiki page",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				client, err := api.NewClient()
				if err != nil {
					return err
				}
				page, err := client.WikiPageAction(args[0], a)
				if err != nil {
					return err
				}
				return output.NewFormatter(config.Cfg.OutputFormat, false).Print(page)
			},
		})
	}
}

func runList(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}
	pages, err := client.ListWikiPages(archived, updatedAfter)
	if err != nil {
		return err
	}
	return output.NewFormatter(config.Cfg.OutputFormat, false).Print(pages)
}

func runShow(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}
	page, err := client.GetWikiPage(args[0])
	if err != nil {
		return err
	}
	return output.NewFormatter(config.Cfg.OutputFormat, false).Print(page)
}

func writePayload(cmd *cobra.Command, requireName bool) (map[string]interface{}, error) {
	payload := map[string]interface{}{}
	if cmd.Flags().Changed("name") {
		payload["name"] = pageName
	}
	if requireName && pageName == "" {
		return nil, fmt.Errorf("--name is required")
	}
	if requireName {
		payload["name"] = pageName
	}
	if cmd.Flags().Changed("html") {
		payload["description_html"] = pageHTML
	}
	if cmd.Flags().Changed("parent") {
		payload["parent"] = pageParent
	}
	if cmd.Flags().Changed("color") {
		payload["color"] = pageColor
	}
	if cmd.Flags().Changed("sort-order") {
		payload["sort_order"] = pageSort
	}
	return payload, nil
}

func runCreate(cmd *cobra.Command, args []string) error {
	payload, err := writePayload(cmd, true)
	if err != nil {
		return err
	}
	client, err := api.NewClient()
	if err != nil {
		return err
	}
	page, err := client.CreateWikiPage(payload)
	if err != nil {
		return err
	}
	return output.NewFormatter(config.Cfg.OutputFormat, false).Print(page)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	payload, err := writePayload(cmd, false)
	if err != nil {
		return err
	}
	if len(payload) == 0 {
		return fmt.Errorf("no fields changed; pass at least one update flag")
	}
	client, err := api.NewClient()
	if err != nil {
		return err
	}
	page, err := client.UpdateWikiPage(args[0], payload)
	if err != nil {
		return err
	}
	return output.NewFormatter(config.Cfg.OutputFormat, false).Print(page)
}
