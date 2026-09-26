# Plane CLI

Go CLI for Plane, optimized for interactive use, cron jobs, and AI agents.

This fork adds instance-wide discovery, reporting, semantic digests, and Wiki support for the service-token APIs implemented in [`r2d-ai/plane`](https://github.com/r2d-ai/plane), while retaining the upstream CLI features for normal workspace/project operations.

## Highlights

### v0.2

- Semantic digests for users, projects, and workspaces
- Automation-stable digest schema with `schema_version`
- Semantic sections: overdue, due today, blocked, stale, recently completed, unassigned
- Timezone-aware due-today evaluation
- Configurable completion lookback and stale threshold
- Per-section output limits for agent/cron token control

### v0.1

- PAT, workspace access token (WSAT), and instance access token (IAT) authentication
- Instance-wide workspace/project discovery with IAT
- Safe workspace-scoped mutations: IAT never auto-selects a workspace for writes
- Work item, project, cycle, module, state, label, comment, attachment, link, time-log, intake, and type commands
- Cross-workspace reports: `summary`, `workload`, and `activity`
- Wiki read/write commands for the Plane fork public Wiki API
- Raw REST API escape hatch for automation
- JSON and YAML output
- Environment-first configuration for cron and agents
- Shell completion and AI-context generation/injection

## Requirements

- Go 1.25+ when installing from source
- A Plane API token:
  - PAT for normal user-scoped access
  - WSAT for one workspace
  - IAT for instance-wide discovery/reporting on the `r2d-ai/plane` fork

## Installation

```bash
go install github.com/ba0f3/plane-cli@latest
```

Or download a Linux, macOS, or Windows archive from:

https://github.com/ba0f3/plane-cli/releases

## Authentication

```bash
plane-cli auth login
plane-cli auth context
```

Service-token example:

```bash
plane-cli auth login \
  --token "$PLANE_API_KEY" \
  --api-host https://work.example.com
```

WSAT automatically resolves its bound workspace from `/api/v1/auth/context/`. IAT intentionally keeps the default workspace empty unless one is explicitly configured.

For upstream Plane instances without the service-token discovery endpoints, login falls back to PAT behavior and requires a workspace.

## Automation / cron configuration

Environment variables override file configuration:

```bash
export PLANE_API_HOST=https://work.example.com
export PLANE_API_KEY=plane_iat_xxx
# PLANE_TOKEN is also accepted as an alias for PLANE_API_KEY

# Optional scope for workspace/project commands
export PLANE_WORKSPACE=engineering
export PLANE_PROJECT=project-uuid
```

`PLANE_BASE_URL` remains supported as a backward-compatible alias for `PLANE_API_HOST`.

Configuration is otherwise stored in:

```text
~/.config/plane-cli/config.yaml
```

A repository/project can also override workspace/project defaults through `.plane/settings.yaml`.

## Instance-wide discovery

With an IAT and no default workspace:

```bash
plane-cli auth context
plane-cli workspace list
plane-cli project list
```

`project list` scans accessible workspaces. Workspace-bound write commands still require an explicit/default workspace and do not fan out automatically.

## Semantic digests

`digest` produces a semantic snapshot intended for cron jobs, email renderers, goclaw, and other agents. It is different from `report activity`: activity is a raw recent-change feed, while digest classifies current work into actionable sections.

### User digest

Scan work assigned to one user. With an IAT and no default workspace this scans all accessible workspaces.

```bash
plane-cli digest user alice@example.com
plane-cli digest user USER_UUID --since 7d
plane-cli digest user "Alice Nguyen" --stale-days 5
```

User selectors match assignee UUID, email, display name, or full name case-insensitively.

### Project digest

```bash
plane-cli digest project GAME
plane-cli digest project PROJECT_UUID
plane-cli --workspace engineering digest project GAME
```

The selector may be project UUID, identifier, or name. If omitted, the configured `PLANE_PROJECT`/default project is used.

### Workspace digest

```bash
plane-cli digest workspace engineering
plane-cli --workspace engineering digest workspace
```

The workspace slug may be omitted when a default workspace is configured.

### Digest flags

```text
--since 24h                  completed-item lookback
--stale-days 7               active item becomes stale after N days without updates
--limit 100                  maximum rows per semantic section; 0 = unlimited
--timezone Asia/Ho_Chi_Minh  timezone used for due-today classification
```

`--since` accepts RFC3339, `YYYY-MM-DD`, Go durations such as `24h`, and day shorthand such as `7d`.

Semantic sections:

- `overdue`: active item with target date before today
- `due_today`: active item due today in the selected timezone
- `blocked`: active item whose state or label is `block`, `blocked`, or `blocker`
- `stale`: active item not updated for at least `--stale-days`
- `completed`: state group `completed` within `--since`; cancelled work is excluded
- `unassigned`: active item with no assignee

Sections are intentionally independent and may overlap. For example, one work item may be both blocked and overdue. Summary counters reflect all matches even when section arrays are truncated by `--limit`.

Example JSON shape:

```json
{
  "schema_version": "1",
  "generated_at": "2026-09-26T09:00:00+07:00",
  "scope": {
    "type": "workspace",
    "selector": "engineering"
  },
  "window": {
    "completed_since": "2026-09-25T09:00:00+07:00",
    "stale_days": 7
  },
  "summary": {
    "matched": 42,
    "active": 31,
    "overdue": 4,
    "due_today": 3,
    "blocked": 2,
    "stale": 6,
    "completed_recent": 11,
    "unassigned": 1
  },
  "sections": {
    "overdue": [],
    "due_today": [],
    "blocked": [],
    "stale": [],
    "completed": [],
    "unassigned": []
  }
}
```

## Reports

### Summary

```bash
plane-cli report summary
plane-cli report summary --since 7d
plane-cli report summary --project GAME
```

Includes item count, active/done counts, completion percentage, overdue, unassigned, and estimate points.

### Workload

```bash
plane-cli report workload
plane-cli report workload --metric points
plane-cli report workload --group-by workspace-assignee
plane-cli report workload --group-by project-assignee --since 30d
```

Metrics: `count`, `points`.

Grouping: `assignee`, `workspace-assignee`, `project-assignee`.

### Activity

Raw recent work-item change feed:

```bash
plane-cli report activity
plane-cli report activity --since 7d
plane-cli report activity --since 2026-09-01 --until 2026-09-26
plane-cli report activity --project GAME --limit 100
```

Default window is the last 24 hours. Rows are sorted by `updated_at` descending. `report digest` remains an alias for backward compatibility; `report activity` is canonical.

## Wiki

The Wiki commands target the public Wiki API implemented in the `r2d-ai/plane` fork.

```bash
plane-cli wiki list
plane-cli wiki show PAGE_ID
plane-cli wiki create --name "Runbook" --html '<p>Hello</p>'
plane-cli wiki update PAGE_ID --name "Updated Runbook"
plane-cli wiki archive PAGE_ID
plane-cli wiki unarchive PAGE_ID
plane-cli wiki lock PAGE_ID
plane-cli wiki unlock PAGE_ID
```

Wiki commands are workspace-scoped. Service-token writes require the `wiki.pages:write` scope on the Plane server.

## Raw API

For endpoints not wrapped by a dedicated command:

```bash
plane-cli raw GET /auth/context/
plane-cli raw GET /workspaces/
plane-cli raw POST /some/path/ -d '{"key":"value"}'
```

Paths are relative to `/api/v1`.

## Common commands

```bash
# Workspaces / projects
plane-cli workspace list
plane-cli workspace switch WORKSPACE_SLUG
plane-cli workspace members
plane-cli project list
plane-cli project info PROJECT_ID
plane-cli project create

# Work items
plane-cli issue list
plane-cli issue view 123
plane-cli issue create --title "Fix login" --priority high
plane-cli issue edit 123 --state STATE_ID
plane-cli issue search "login"

# Cycles / modules
plane-cli cycle list
plane-cli module list

# Collaboration
plane-cli issue comment list 123
plane-cli issue comment add 123 --text "Ready for review"
plane-cli issue activity list 123
plane-cli issue attachment list 123
plane-cli issue link list 123
plane-cli issue time total 123

# Workflow metadata
plane-cli state list
plane-cli label list
plane-cli intake list
plane-cli type list
```

Run `plane-cli <command> --help` for command-specific options.

## Output

Supported structured formats are JSON and YAML:

```bash
plane-cli project list --output json
plane-cli digest workspace engineering --output yaml
```

Table output from the original upstream CLI is not supported in this fork.

## Agent support

```bash
plane-cli context
plane-cli context --all
plane-cli inject
plane-cli inject --dry-run
```

## Development

```bash
make build
make test
make check
```

CI runs build, unit tests, `go vet`, `gofmt`, and `golangci-lint`.

## Release

Tags matching `v*` trigger GitHub Actions + GoReleaser and publish cross-platform archives and checksums.

## License

MIT. See [LICENSE](LICENSE).

## Acknowledgments

This project builds on the Go CLI originally created by [`rohithmahesh3/plane-cli`](https://github.com/rohithmahesh3/plane-cli) and extends it for the `r2d-ai/plane` fork's service-token, Wiki, reporting, and automation workflows.
