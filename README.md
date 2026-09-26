# Plane CLI

Go CLI for Plane, optimized for both interactive use and automation/agents.

This fork adds instance-wide discovery and reporting for the service-token APIs implemented in [`r2d-ai/plane`](https://github.com/r2d-ai/plane), while retaining the upstream CLI features for normal workspace/project operations.

## v0.1.0 highlights

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

### Go install

```bash
go install github.com/ba0f3/plane-cli@latest
```

### Release binaries

Download Linux, macOS, or Windows binaries from GitHub Releases:

https://github.com/ba0f3/plane-cli/releases

## Authentication

Interactive login:

```bash
plane-cli auth login
```

Service-token examples:

```bash
# WSAT: workspace is auto-discovered from /api/v1/auth/context/
plane-cli auth login --token "$PLANE_API_KEY" --api-host https://work.example.com

# IAT: intentionally leaves the default workspace empty
plane-cli auth login --token "$PLANE_API_KEY" --api-host https://work.example.com
```

Inspect the authenticated principal and token scope:

```bash
plane-cli auth context
```

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

## Reports

### Summary

Workspace/project work-item overview:

```bash
plane-cli report summary
plane-cli report summary --since 7d
plane-cli report summary --project GAME
```

Includes item count, active/done counts, completion percentage, overdue, unassigned, and estimate points.

### Workload

Assignee workload distribution:

```bash
plane-cli report workload
plane-cli report workload --metric points
plane-cli report workload --group-by workspace-assignee
plane-cli report workload --group-by project-assignee --since 30d
```

Supported metrics:

- `count`
- `points`

Supported grouping:

- `assignee`
- `workspace-assignee`
- `project-assignee`

### Activity

Recent work-item activity feed for scripts and agents:

```bash
plane-cli report activity
plane-cli report activity --since 7d
plane-cli report activity --since 2026-09-01 --until 2026-09-26
plane-cli report activity --project GAME --limit 100
```

Default window is the last 24 hours. Rows are sorted by `updated_at` descending.

`report digest` is retained as an alias for backward compatibility, but `report activity` is the canonical command.

Common report time filters accept:

- RFC3339
- `YYYY-MM-DD`
- Go durations such as `24h`
- day shorthand such as `7d`

Use `--date-field updated|created` to select the timestamp used for filtering.

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

Wiki commands are workspace-scoped. Service-token writes require the appropriate `wiki.pages:write` scope on the Plane server.

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
plane-cli cycle create --name "Sprint 1" --start-date 2026-09-01 --end-date 2026-09-14
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

Run `plane-cli <command> --help` for the complete command-specific options.

## Output

Supported structured formats are JSON and YAML:

```bash
plane-cli project list --output json
plane-cli report summary --output yaml
```

Table output from the original upstream CLI is not supported in this fork.

## Agent support

Generate CLI command context:

```bash
plane-cli context
plane-cli context --all
```

Inject the generated context into supported agent files:

```bash
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
