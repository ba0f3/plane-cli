package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
)

type App struct {
	Out io.Writer
	Err io.Writer

	baseURL   string
	token     string
	workspace string
	config    string
	format    string

	loaded     bool
	cfg        config.Config
	configPath string
	client     *api.Client
}

func Run(args []string, out, errOut io.Writer) error {
	a := &App{Out: out, Err: errOut, format: "table"}
	args, err := a.extractGlobals(args)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		a.printRootHelp()
		return nil
	}
	cmd := normalizeCommand(args[0])
	rest := args[1:]
	switch cmd {
	case "help", "-h", "--help":
		a.printRootHelp()
		return nil
	case "configure":
		return a.runConfigure(rest)
	case "auth":
		return a.runAuth(rest)
	case "workspace":
		return a.runWorkspace(rest)
	case "project":
		return a.runProject(rest)
	case "work-item":
		return a.runWorkItem(rest)
	case "cycle", "module", "state", "label":
		return a.runProjectResource(cmd, rest)
	case "member":
		return a.runMember(rest)
	case "wiki":
		return a.runWiki(rest)
	case "report":
		return a.runReport(rest)
	case "raw":
		return a.runRaw(rest)
	default:
		return fmt.Errorf("unknown command %q; run `plane help`", args[0])
	}
}

func normalizeCommand(v string) string {
	switch v {
	case "ws", "workspaces":
		return "workspace"
	case "projects":
		return "project"
	case "wi", "issue", "issues":
		return "work-item"
	case "cycles":
		return "cycle"
	case "modules":
		return "module"
	case "states":
		return "state"
	case "labels":
		return "label"
	case "members", "user", "users":
		return "member"
	default:
		return v
	}
}

func (a *App) extractGlobals(args []string) ([]string, error) {
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, val, has := splitFlag(arg)
		isGlobal := false
		switch name {
		case "--base-url":
			isGlobal = true
			a.baseURL = val
		case "--token":
			isGlobal = true
			a.token = val
		case "--workspace", "-w":
			isGlobal = true
			a.workspace = val
		case "--config":
			isGlobal = true
			a.config = val
		case "--output", "-o":
			isGlobal = true
			a.format = val
		}
		if !isGlobal {
			rest = append(rest, arg)
			continue
		}
		if !has {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("%s requires a value", name)
			}
			i++
			val = args[i]
			switch name {
			case "--base-url":
				a.baseURL = val
			case "--token":
				a.token = val
			case "--workspace", "-w":
				a.workspace = val
			case "--config":
				a.config = val
			case "--output", "-o":
				a.format = val
			}
		}
	}
	return rest, nil
}

func splitFlag(arg string) (name, val string, has bool) {
	if i := strings.Index(arg, "="); i > 0 && strings.HasPrefix(arg, "-") {
		return arg[:i], arg[i+1:], true
	}
	return arg, "", false
}

func (a *App) load() error {
	if a.loaded {
		return nil
	}
	cfg, path, err := config.Load(config.Overrides{BaseURL: a.baseURL, Token: a.token, Workspace: a.workspace, Config: a.config})
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	a.cfg, a.configPath = cfg, path
	a.client = api.New(cfg.BaseURL, cfg.Token)
	a.loaded = true
	return nil
}
func (a *App) ctxClient() (context.Context, *api.Client, error) {
	if err := a.load(); err != nil {
		return nil, nil, err
	}
	return context.Background(), a.client, nil
}

func (a *App) resolveWorkspaces(ctx context.Context, forWrite bool) ([]api.Workspace, error) {
	if err := a.load(); err != nil {
		return nil, err
	}
	items, err := a.client.Workspaces(ctx)
	if err != nil {
		return nil, err
	}
	if s := strings.TrimSpace(a.cfg.Workspace); s != "" {
		w, err := matchWorkspace(items, s)
		if err != nil {
			return nil, err
		}
		return []api.Workspace{w}, nil
	}
	if forWrite && len(items) != 1 {
		return nil, fmt.Errorf("write command needs exactly one workspace; use --workspace (token can access %d)", len(items))
	}
	return items, nil
}
func (a *App) resolveProject(ctx context.Context, ws api.Workspace, selector string) (api.Object, error) {
	items, err := a.client.ListObjects(ctx, fmt.Sprintf("/api/v1/workspaces/%s/projects/", url.PathEscape(ws.Slug)), nil)
	if err != nil {
		return nil, err
	}
	return matchObject(items, selector, []string{"id", "identifier", "name"}, "project")
}

type projectScope struct {
	Workspace api.Workspace
	Project   api.Object
}

func (a *App) scopesForProjects(ctx context.Context, selector string) ([]projectScope, error) {
	wss, err := a.resolveWorkspaces(ctx, false)
	if err != nil {
		return nil, err
	}
	var out []projectScope
	for _, ws := range wss {
		items, err := a.client.ListObjects(ctx, fmt.Sprintf("/api/v1/workspaces/%s/projects/", url.PathEscape(ws.Slug)), nil)
		if err != nil {
			return nil, fmt.Errorf("workspace %s: %w", ws.Slug, err)
		}
		for _, p := range items {
			if selector != "" && !objectMatches(p, selector, []string{"id", "identifier", "name"}) {
				continue
			}
			out = append(out, projectScope{ws, p})
		}
	}
	if selector != "" && len(out) == 0 {
		return nil, fmt.Errorf("project %q not found in selected workspace scope", selector)
	}
	return out, nil
}
func matchWorkspace(items []api.Workspace, s string) (api.Workspace, error) {
	for _, w := range items {
		if strings.EqualFold(w.Slug, s) || strings.EqualFold(w.ID, s) || strings.EqualFold(w.Name, s) {
			return w, nil
		}
	}
	needle := strings.ToLower(strings.TrimSpace(s))
	var c []api.Workspace
	for _, w := range items {
		if strings.Contains(strings.ToLower(w.Slug), needle) || strings.Contains(strings.ToLower(w.Name), needle) {
			c = append(c, w)
		}
	}
	if len(c) == 1 {
		return c[0], nil
	}
	if len(c) > 1 {
		names := make([]string, 0, len(c))
		for _, w := range c {
			names = append(names, w.Slug)
		}
		sort.Strings(names)
		return api.Workspace{}, fmt.Errorf("workspace %q is ambiguous: %s", s, strings.Join(names, ", "))
	}
	return api.Workspace{}, fmt.Errorf("workspace %q not found", s)
}
func matchObject(items []api.Object, s string, keys []string, kind string) (api.Object, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("%s selector is required", kind)
	}
	for _, it := range items {
		for _, k := range keys {
			if strings.EqualFold(api.String(it, k), s) {
				return it, nil
			}
		}
	}
	var m []api.Object
	for _, it := range items {
		if objectMatches(it, s, keys) {
			m = append(m, it)
		}
	}
	if len(m) == 1 {
		return m[0], nil
	}
	if len(m) > 1 {
		return nil, fmt.Errorf("%s %q is ambiguous (%d matches)", kind, s, len(m))
	}
	return nil, fmt.Errorf("%s %q not found", kind, s)
}
func objectMatches(it api.Object, s string, keys []string) bool {
	n := strings.ToLower(strings.TrimSpace(s))
	if n == "" {
		return true
	}
	for _, k := range keys {
		v := strings.ToLower(api.String(it, k))
		if v == n || strings.Contains(v, n) {
			return true
		}
	}
	return false
}
func requireExactlyOneWorkspace(items []api.Workspace) (api.Workspace, error) {
	if len(items) != 1 {
		return api.Workspace{}, fmt.Errorf("expected exactly one workspace, got %d; use --workspace", len(items))
	}
	return items[0], nil
}
func isHTTPStatus(err error, status int) bool {
	var e *api.HTTPError
	return errors.As(err, &e) && e.StatusCode == status
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var flags, positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
		f := fs.Lookup(name)
		if f == nil {
			return nil, fmt.Errorf("unknown flag %s", arg)
		}
		flags = append(flags, arg)
		if !strings.Contains(arg, "=") {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("flag %s requires value", arg)
			}
			i++
			flags = append(flags, args[i])
		}
	}
	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return positionals, nil
}

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	for _, x := range strings.Split(v, ",") {
		x = strings.TrimSpace(x)
		if x != "" {
			*s = append(*s, x)
		}
	}
	return nil
}
func aliasString(fs *flag.FlagSet, long, short, def, usage string, target *string) {
	fs.StringVar(target, long, def, usage)
	if short != "" {
		fs.StringVar(target, short, def, usage)
	}
}
func aliasInt(fs *flag.FlagSet, long, short string, def int, usage string, target *int) {
	fs.IntVar(target, long, def, usage)
	if short != "" {
		fs.IntVar(target, short, def, usage)
	}
}
func aliasSlice(fs *flag.FlagSet, long, short, usage string, target *stringSlice) {
	fs.Var(target, long, usage)
	if short != "" {
		fs.Var(target, short, usage)
	}
}

func (a *App) printRootHelp() {
	fmt.Fprintln(a.Out, `plane - Go CLI for Plane

Usage:
  plane [global flags] <command> [args]

Commands:
  configure               Save base URL/token/default workspace
  auth context            Show PAT/WSAT/IAT context
  workspace list          Discover token-visible workspaces
  project list|show       Projects across workspace scope
  work-item ...           List/show/create/update work items
  cycle|module|state|label list -p PROJECT
  member list             Workspace members
  wiki ...                Workspace Wiki pages
  report summary          Cross-workspace metrics
  report workload         Assignee workload and share
  report digest           Daily/weekly recent-change digest
  raw METHOD PATH         Direct API fallback

Global flags (accepted anywhere):
  --base-url URL   --token TOKEN   -w, --workspace SLUG
  -o, --output table|json|csv     --config PATH

Environment: PLANE_BASE_URL, PLANE_TOKEN (or PLANE_API_KEY), PLANE_WORKSPACE.`)
}

func defaultConfigPath() (string, error) { return config.DefaultPath() }
func fileExists(path string) bool        { _, err := os.Stat(path); return err == nil }
