package cli

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ba0f3/plane-cli/internal/output"
	"github.com/ba0f3/plane-cli/internal/report"
)

type reportFlags struct{ project, assignee, state, priority, since, until, dateField string }

func addReportFlags(fs interface {
	StringVar(*string, string, string, string)
}, f *reportFlags, defaultSince string) {
	fs.StringVar(&f.project, "project", "", "Project")
	fs.StringVar(&f.assignee, "assignee", "", "Assignee")
	fs.StringVar(&f.state, "state", "", "State")
	fs.StringVar(&f.priority, "priority", "", "Priority")
	fs.StringVar(&f.since, "since", defaultSince, "Since")
	fs.StringVar(&f.until, "until", "", "Until")
	fs.StringVar(&f.dateField, "date-field", "updated", "updated|created|target")
}
func (a *App) runReport(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: plane report summary|workload|digest")
	}
	switch args[0] {
	case "summary":
		return a.runReportSummary(args[1:])
	case "workload":
		return a.runReportWorkload(args[1:])
	case "digest":
		return a.runReportDigest(args[1:])
	default:
		return fmt.Errorf("unknown report %q", args[0])
	}
}
func (a *App) collectForReport(f reportFlags) ([]report.Item, error) {
	ctx, _, err := a.ctxClient()
	if err != nil {
		return nil, err
	}
	scopes, err := a.scopesForProjects(ctx, f.project)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	st, err := report.ParseTime(f.since, now)
	if err != nil {
		return nil, err
	}
	ut, err := report.ParseTime(f.until, now)
	if err != nil {
		return nil, err
	}
	rsc := make([]report.Scope, 0, len(scopes))
	for _, s := range scopes {
		rsc = append(rsc, report.Scope{Workspace: s.Workspace, Project: s.Project})
	}
	return (report.Collector{Client: a.client, Concurrency: 8}).Collect(ctx, rsc, report.Filter{Since: st, Until: ut, DateField: f.dateField, Assignee: f.assignee, State: f.state, Priority: f.priority})
}
func (a *App) runReportSummary(args []string) error {
	fs := newFlagSet("report summary")
	var f reportFlags
	addReportFlags(fs, &f, "")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(pos, " "))
	}
	items, err := a.collectForReport(f)
	if err != nil {
		return err
	}
	type agg struct {
		workspace, project                            string
		total, completed, active, overdue, unassigned int
		points                                        float64
	}
	m := map[string]*agg{}
	now := time.Now()
	for _, it := range items {
		k := it.Workspace + "\x00" + it.Project
		x := m[k]
		if x == nil {
			x = &agg{workspace: it.Workspace, project: it.Project}
			m[k] = x
		}
		x.total++
		x.points += it.Point
		if it.StateGroup == "completed" || it.StateGroup == "cancelled" {
			x.completed++
		} else {
			x.active++
		}
		if len(it.Assignees) == 0 {
			x.unassigned++
		}
		if it.HasTargetTime && it.TargetTime.Before(now) && it.StateGroup != "completed" && it.StateGroup != "cancelled" {
			x.overdue++
		}
	}
	rows := make([]output.Row, 0, len(m))
	for _, x := range m {
		pct := 0.0
		if x.total > 0 {
			pct = 100 * float64(x.completed) / float64(x.total)
		}
		rows = append(rows, output.Row{"workspace": x.workspace, "project": x.project, "items": x.total, "active": x.active, "completed": x.completed, "completion": fmt.Sprintf("%.1f%%", pct), "overdue": x.overdue, "unassigned": x.unassigned, "points": trimFloat(x.points)})
	}
	sort.Slice(rows, func(i, j int) bool {
		return fmt.Sprint(rows[i]["workspace"], rows[i]["project"]) < fmt.Sprint(rows[j]["workspace"], rows[j]["project"])
	})
	return output.Render(a.Out, a.format, rows, []output.Column{{Key: "workspace", Title: "WORKSPACE"}, {Key: "project", Title: "PROJECT"}, {Key: "items", Title: "ITEMS"}, {Key: "active", Title: "ACTIVE"}, {Key: "completed", Title: "DONE"}, {Key: "completion", Title: "DONE %"}, {Key: "overdue", Title: "OVERDUE"}, {Key: "unassigned", Title: "UNASSIGNED"}, {Key: "points", Title: "POINTS"}})
}
func (a *App) runReportWorkload(args []string) error {
	fs := newFlagSet("report workload")
	var f reportFlags
	addReportFlags(fs, &f, "")
	var metric, groupBy string
	fs.StringVar(&metric, "metric", "count", "count|points")
	fs.StringVar(&groupBy, "group-by", "assignee", "assignee|workspace-assignee|project-assignee")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(pos, " "))
	}
	if metric != "count" && metric != "points" {
		return fmt.Errorf("invalid --metric %q", metric)
	}
	items, err := a.collectForReport(f)
	if err != nil {
		return err
	}
	type agg struct {
		workspace, project, assignee string
		count                        int
		points, value                float64
	}
	m := map[string]*agg{}
	for _, it := range items {
		as := it.Assignees
		if len(as) == 0 {
			as = []string{"(unassigned)"}
		}
		for _, name := range as {
			var key string
			var x *agg
			switch groupBy {
			case "assignee":
				key = name
				x = &agg{assignee: name}
			case "workspace-assignee":
				key = it.Workspace + "\x00" + name
				x = &agg{workspace: it.Workspace, assignee: name}
			case "project-assignee", "assignee-project":
				key = it.Workspace + "\x00" + it.Project + "\x00" + name
				x = &agg{workspace: it.Workspace, project: it.Project, assignee: name}
			default:
				return fmt.Errorf("invalid --group-by %q", groupBy)
			}
			if old := m[key]; old != nil {
				x = old
			} else {
				m[key] = x
			}
			x.count++
			x.points += it.Point
		}
	}
	total := 0.0
	for _, x := range m {
		if metric == "points" {
			x.value = x.points
		} else {
			x.value = float64(x.count)
		}
		total += x.value
	}
	rows := make([]output.Row, 0, len(m))
	for _, x := rane m {
		pct := 0.0
		if total > 0 {
			pct = 100 * x.value / total
		}
		rows = append(rows, output.Row{"workspace": x.workspace, "project": x.project, "assignee": x.assignee, "items": x.count, "points": trimFloat(x.points), "value": x.value, "share": fmt.Sprintf("%.1f%%", pct)})
	}
	sort.Slice(rows, func(i, j int) bool { return asFloat(rows[i]["value"]) > asFloat(rows[j]["value"]) })
	return output.Render(a.Out, a.format, rows, []output.Column{{Key: "workspace", Title: "WORKSPACE"}, {Key: "project", Title: "PROJECT"}, {Key: "assignee", Title: "ASSIGNEE"}, {Key: "items", Title: "ITEMS"}, {Key: "points", Title: "POINTS"}, {Key: "share", Title: "SHARE"}})
}
func (a *App) runReportDigest(args []string) error {
	fs := newFlagSet("report digest")
	var f reportFlags
	addReportFlags(fs, &f, "24h")
	var limit int
	aliasInt(fs, "limit", "l", 200, "Limit", &limit)
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(pos, " "))
	}
	items, err := a.collectForReport(f)
	if err != nil {
		return err
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	rows := make([]output.Row, 0, len(items))
	for _, it := range items {
		rows = append(rows, itemRow(it))
	}
	return output.Render(a.Out, a.format, rows, workItemColumns())
}
func trimFloat(v float64) any {
	if math.Abs(v-math.Round(v)) < 1e-9 {
		return int64(math.Round(v))
	}
	return fmt.Sprintf("%.2f", v)
}
func asFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}
