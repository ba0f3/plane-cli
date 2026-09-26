package wiki

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/ba0f3/plane-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	archived     bool
	updatedAfter string
	listFull     bool
	pageName     string
	pageContent  string
	pageFile     string
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
	list := &cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List Wiki page summaries", RunE: runList}
	show := &cobra.Command{Use: "show <page-id>", Aliases: []string{"get", "view"}, Short: "Show one Wiki page with full content", Args: cobra.ExactArgs(1), RunE: runShow}
	create := &cobra.Command{Use: "create", Short: "Create a Wiki page from native Markdown or raw HTML", RunE: runCreate}
	update := &cobra.Command{Use: "update <page-id>", Short: "Update a Wiki page from native Markdown or raw HTML", Args: cobra.ExactArgs(1), RunE: runUpdate}

	list.Flags().BoolVar(&archived, "archived", false, "List archived pages")
	list.Flags().StringVar(&updatedAfter, "updated-after", "", "Only pages updated after ISO-8601 timestamp")
	list.Flags().BoolVar(&listFull, "full", false, "Include full page content in list output")

	for _, c := range []*cobra.Command{create, update} {
		c.Flags().StringVar(&pageName, "name", "", "Page name")
		c.Flags().StringVar(&pageContent, "content", "", "Page content in Markdown; sent to Plane without client-side rendering")
		c.Flags().StringVar(&pageFile, "file", "", "Read Markdown directly from file; use - for stdin")
		c.Flags().StringVar(&pageHTML, "html", "", "Raw HTML content (compatibility/escape hatch)")
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
	if !listFull {
		pages = summarizeWikiPages(pages)
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

func summarizeWikiPages(pages []api.WikiPage) []api.WikiPage {
	out := make([]api.WikiPage, 0, len(pages))
	for _, page := range pages {
		summary := make(api.WikiPage, len(page))
		for key, value := range page {
			if isWikiContentField(key) {
				continue
			}
			summary[key] = value
		}
		out = append(out, summary)
	}
	return out
}

func isWikiContentField(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "description", "description_markdown", "description_html", "description_json", "description_binary",
		"content", "content_markdown", "content_html", "content_json", "content_binary",
		"body", "body_markdown", "body_html", "body_json":
		return true
	default:
		return false
	}
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

	bodyField, body, bodySet, err := resolvePageBody(cmd)
	if err != nil {
		return nil, err
	}
	if bodySet {
		payload[bodyField] = body
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

func resolvePageBody(cmd *cobra.Command) (string, string, bool, error) {
	return pageBody(
		pageContent, cmd.Flags().Changed("content"),
		pageFile, cmd.Flags().Changed("file"),
		pageHTML, cmd.Flags().Changed("html"),
		cmd.InOrStdin(),
	)
}

func pageBody(content string, contentSet bool, filePath string, fileSet bool, rawHTML string, htmlSet bool, stdin io.Reader) (string, string, bool, error) {
	sources := 0
	for _, set := range []bool{contentSet, fileSet, htmlSet} {
		if set {
			sources++
		}
	}
	if sources == 0 {
		return "", "", false, nil
	}
	if sources > 1 {
		return "", "", false, fmt.Errorf("use only one of --content, --file, or --html")
	}

	if htmlSet {
		return "description_html", rawHTML, true, nil
	}

	markdownContent := content
	if fileSet {
		if strings.TrimSpace(filePath) == "" {
			return "", "", false, fmt.Errorf("--file requires a path or - for stdin")
		}
		var data []byte
		var err error
		if filePath == "-" {
			data, err = io.ReadAll(stdin)
		} else {
			data, err = os.ReadFile(filePath)
		}
		if err != nil {
			return "", "", false, fmt.Errorf("read wiki content %q: %w", filePath, err)
		}
		markdownContent = string(data)
	}

	return "description_markdown", markdownContent, true, nil
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
