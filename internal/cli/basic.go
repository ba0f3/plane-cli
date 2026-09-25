package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/ba0f3/plane-cli/internal/output"
)

func (a *App) runConfigure(args []string) error {
	fs := newFlagSet("configure")
	var baseURL, token, workspace, path string
	fs.StringVar(&baseURL, "base-url", "", "Plane base URL")
	fs.StringVar(&token, "token", "", "Plane token")
	aliasString(fs, "workspace", "w", "", "Default workspace", &workspace)
	fs.StringVar(&path, "path", "", "Config path")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(pos, " "))
	}
	if baseURL == "" || token == "" {
		return fmt.Errorf("--base-url and --token are required")
	}
	if path == "" {
		path, err = config.DefaultPath()
		if err != nil {
			return err
		}
	}
	if err := config.Save(path, config.Config{BaseURL: baseURL, Token: token, Workspace: workspace}); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "saved config to %s\n", path)
	return nil
}
func (a *App) runAuth(args []string) error {
	if len(args) != 1 || args[0] != "context" {
		return fmt.Errorf("usage: plane auth context")
	}
	ctx, c, err := a.ctxClient()
	if err != nil {
		return err
	}
	v, err := c.AuthContext(ctx)
	if err != nil {
		return err
	}
	return output.RenderObject(a.Out, "json", v)
}
func (a *App) runWorkspace(args []string) error {
	if len(args) != 1 || (args[0] != "list" && args[0] != "ls") {
		return fmt.Errorf("usage: plane workspace list")
	}
	ctx, c, err := a.ctxClient()
	if err != nil {
		return err
	}
	items, err := c.Workspaces(ctx)
	if err != nil {
		return err
	}
	rows := make([]output.Row, 0, len(items))
	for _, w := range items {
		rows = append(rows, output.Row{"slug": w.Slug, "name": w.Name, "id": w.ID})
	}
	return output.Render(a.Out, a.format, rows, []output.Column{{Key: "slug", Title: "SLUG"}, {Key: "name", Title: "NAME"}, {Key: "id", Title: "ID"}})
}

func (a *App) runProject(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: plane project list|show")
	}
	sub := args[0]
	rest := args[1:]
	ctx, _, err := a.ctxClient()
	if err != nil {
		return err
	}
	switch sub {
	case "list", "ls":
		if len(rest) > 0 {
			return fmt.Errorf("usage: plane project list")
		}
		wss, err := a.resolveWorkspaces(ctx, false)
		if err != nil {
			return err
		}
		var rows []output.Row
		for _, ws := range wss {
			ps, err := a.client.ListObjects(ctx, fmt.Sprintf("/api/v1/workspaces/%s/projects/", url.PathEscape(ws.Slug)), nil)
			if err != nil {
				return err
			}
			for _, p := range ps {
				rows = append(rows, output.Row{"workspace": ws.Slug, "identifier": api.String(p, "identifier"), "name": api.String(p, "name"), "members": api.String(p, "total_members"), "id": api.String(p, "id")})
			}
		}
		sort.SliceStable(rows, func(i, j int) bool {
			return fmt.Sprint(rows[i]["workspace"], rows[i]["identifier"]) < fmt.Sprint(rows[j]["workspace"], rows[j]["identifier"])
		})
		return output.Render(a.Out, a.format, rows, []output.Column{{Key: "workspace", Title: "WORKSPACE"}, {Key: "identifier", Title: "KEY"}, {Key: "name", Title: "NAME"}, {Key: "members", Title: "MEMBERS"}, {Key: "id", Title: "ID"}})
	case "show":
		if len(rest) != 1 {
			return fmt.Errorf("usage: plane project show <project>")
		}
		wss, err := a.resolveWorkspaces(ctx, false)
		if err != nil {
			return err
		}
		for _, ws := range wss {
			p, e := a.resolveProject(ctx, ws, rest[0])
			if e == nil {
				p["workspace_slug"] = ws.Slug
				return output.RenderObject(a.Out, "json", p)
			}
		}
		return fmt.Errorf("project %q not found", rest[0])
	default:
		return fmt.Errorf("unknown project subcommand %q", sub)
	}
}

func (a *App) runProjectResource(resource string, args []string) error {
	if len(args) == 0 || (args[0] != "list" && args[0] != "ls") {
		return fmt.Errorf("usage: plane %s list --project PROJECT", resource)
	}
	fs := newFlagSet(resource + " list")
	var project string
	aliasString(fs, "project", "p", "", "Project", &project)
	pos, err := parseInterspersed(fs, args[1:])
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(pos, " "))
	}
	if project == "" {
		return fmt.Errorf("--project is required")
	}
	ctx, _, err := a.ctxClient()
	if err != nil {
		return err
	}
	wss, err := a.resolveWorkspaces(ctx, false)
	if err != nil {
		return err
	}
	var rows []output.Row
	for _, ws := range wss {
		p, e := a.resolveProject(ctx, ws, project)
		if e != nil {
			continue
		}
		path := fmt.Sprintf("/api/v1/workspaces/%s/projects/%s/%ss/", url.PathEscape(ws.Slug), url.PathEscape(api.String(p, "id")), resource)
		if resource == "state" {
			path = fmt.Sprintf("/api/v1/workspaces/%s/projects/%s/states/", url.PathEscape(ws.Slug), url.PathEscape(api.String(p, "id")))
		}
		items, e := a.client.ListObjects(ctx, path, nil)
		if e != nil {
			return e
		}
		for _, it := range items {
			rows = append(rows, output.Row{"workspace": ws.Slug, "project": api.String(p, "identifier"), "name": api.String(it, "name", "display_name"), "status": api.String(it, "status", "group"), "start": api.String(it, "start_date"), "target": api.String(it, "target_date", "end_date"), "id": api.String(it, "id")})
		}
	}
	return output.Render(a.Out, a.format, rows, []output.Column{{Key: "workspace", Title: "WORKSPACE"}, {Key: "project", Title: "PROJECT"}, {Key: "name", Title: "NAME"}, {Key: "status", Title: "STATUS"}, {Key: "start", Title: "START"}, {Key: "target", Title: "TARGET"}, {Key: "id", Title: "ID"}})
}

func (a *App) runMember(args []string) error {
	if len(args) != 1 || (args[0] != "list" && args[0] != "ls") {
		return fmt.Errorf("usage: plane member list")
	}
	ctx, _, err := a.ctxClient()
	if err != nil {
		return err
	}
	wss, err := a.resolveWorkspaces(ctx, false)
	if err != nil {
		return err
	}
	var rows []output.Row
	for _, ws := range wss {
		items, err := a.client.ListObjects(ctx, fmt.Sprintf("/api/v1/workspaces/%s/members/", url.PathEscape(ws.Slug)), nil)
		if err != nil {
			return err
		}
		for _, it := range items {
			name := api.String(it, "display_name", "name")
			if name == "" {
				name = strings.TrimSpace(api.String(it, "first_name") + " " + api.String(it, "last_name"))
			}
			rows = append(rows, output.Row{"workspace": ws.Slug, "name": name, "email": api.String(it, "email"), "role": api.String(it, "role"), "id": api.String(it, "id")})
		}
	}
	return output.Render(a.Out, a.format, rows, []output.Column{{Key: "workspace", Title: "WORKSPACE"}, {Key: "name", Title: "NAME"}, {Key: "email", Title: "EMAIL"}, {Key: "role", Title: "ROLE"}, {Key: "id", Title: "ID"}})
}

func (a *App) runRaw(args []string) error {
	fs := newFlagSet("raw")
	var data string
	var query stringSlice
	fs.StringVar(&data, "data", "", "JSON body")
	fs.StringVar(&data, "d", "", "JSON body")
	fs.Var(&query, "query", "key=value")
	fs.Var(&query, "q", "key=value")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return fmt.Errorf("usage: plane raw METHOD PATH [--data JSON] [--query k=v]")
	}
	ctx, c, err := a.ctxClient()
	if err != nil {
		return err
	}
	q := url.Values{}
	for _, pair := range query {
		p := strings.SplitN(pair, "=", 2)
		if len(p) != 2 {
			return fmt.Errorf("invalid query %q", pair)
		}
		q.Add(p[0], p[1])
	}
	var body any
	if strings.TrimSpace(data) != "" {
		if err := json.Unmarshal([]byte(data), &body); err != nil {
			return err
		}
	}
	var out any
	if err := c.Request(ctx, strings.ToUpper(pos[0]), pos[1], q, body, &out); err != nil {
		return err
	}
	return output.RenderObject(a.Out, "json", out)
}

var _ = flag.ErrHelp
