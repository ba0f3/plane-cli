# plane-cli

A zero-dependency Go CLI for managing and reporting on Plane, designed for self-hosted Plane and the `r2d-ai/plane` fork.

The key difference from most Plane CLIs is that **workspace is not a mandatory global context**. When authenticated with an instance access token (IAT), read/report commands discover all accessible workspaces through `GET /api/v1/workspaces/` and aggregate them automatically. Write commands deliberately require a single resolved workspace.

## Why

Plane's MCP integration still requires callers to send a workspace slug even when the credential is instance-scoped. That makes company-wide agents, daily digests, weekly reports, and cross-workspace automation awkward.

`plane-cli` uses the public REST API directly and understands the service-token extensions in `r2d-ai/plane`:

- `GET /api/v1/auth/context/`
- `GET /api/v1/workspaces/`
- workspace access tokens (WSAT)
- instance access tokens (IAT)
- `wiki.pages:read` / `wiki.pages:write`

It remains usable with a PAT as long as the token can access the requested public API endpoints.

## Build

```bash
git clone https://github.com/ba0f3/plane-cli.git
cd plane-cli
go build -o plane ./cmd/plane
```

No third-party Go modules are required.

## Configure

```bash
./plane configure \
  --base-url https://work.example.com \
  --token 'plane_iat_...' 
```

Optional default workspace:

```bash
./plane configure \
  --base-url https://work.example.com \
  --token 'plane_iat_...' \
  --workspace tech
```

Config is stored at `$XDG_CONFIG_HOME/plane-cli/config.json` (or the OS config directory) with mode `0600`.

Environment variables override the file:

```bash
export PLANE_BASE_URL=https://work.example.com
export PLANE_TOKEN='plane_iat_...'
# PLANE_API_KEY is also accepted
export PLANE_WORKSPACE=tech   # optional
```

CLI flags override environment/config and may appear anywhere in the command:

```bash
plane report digest --since 24h --workspace tech --output json
```

## Token discovery

```bash
plane auth context
plane workspace list
```

For an IAT, `workspace list` returns all active workspaces allowed by the token. For a WSAT it returns the bound workspace.

## Projects

```bash
plane project list
plane project show ENG
plane -w tech project list
```

With an IAT and no default workspace, `project list` scans every discovered workspace.

## Work items

```bash
# Cross-workspace read
plane work-item list
plane wi list --project ENG --state started --since 7d
plane wi list --assignee Alex --output json

# Show; project can be inferred from ENG-123
plane wi show ENG-123 -w tech

# Create/update: writes require one workspace
plane wi create "Fix login timeout" -w tech -p ENG \
  --priority high --assignee Alex --label bug

plane wi update ENG-123 -w tech --state Done --priority medium
```

Supported list filters:

- `--project`
- `--assignee`
- `--state` (state name or group)
- `--priority`
- `--since`, `--until`
- `--date-field updated|created|target`
- `--limit`

Time syntax accepts RFC3339, `YYYY-MM-DD`, Go duration (`24h`) and day duration (`7d`).

## Resource inspection

```bash
plane cycle list -w tech -p ENG
plane module list -w tech -p ENG
plane state list -w tech -p ENG
plane label list -w tech -p ENG
plane member list -w tech
```

## Wiki

The fork exposes service-token Wiki APIs. Read commands can scan multiple workspaces; writes require one workspace.

```bash
plane wiki list
plane wiki list -w tech --updated-after 2026-09-25T00:00:00Z
plane wiki show <page-id> -w tech

plane wiki create "Ops Runbook" -w tech --content '<p>...</p>'
plane wiki update <page-id> -w tech --title "New title"
plane wiki archive <page-id> -w tech
plane wiki unarchive <page-id> -w tech
plane wiki lock <page-id> -w tech
plane wiki unlock <page-id> -w tech
```

## Reports

### Summary

```bash
# Entire instance for an IAT
plane report summary

# One workspace / last 7 days / based on updated_at
plane report summary -w tech --since 7d
```

Columns include item count, active/done counts, completion %, overdue, unassigned, and total estimate points.

### Workload

```bash
# % workload by assignee using work-item count
plane report workload --since 30d

# Use estimate points instead
plane report workload --metric points --since 30d

# Keep project dimension
plane report workload --group-by project-assignee --since 30d
```

### Daily/weekly digest

```bash
plane report digest                 # defaults to last 24h
plane report digest --since 7d
plane report digest --since 7d --output json
```

This is intended for cron/agents that need a stable non-MCP data source.

## Output

Global output formats:

```bash
plane project list --output table
plane project list --output json
plane project list --output csv
```

`auth context`, `show`, writes, lifecycle operations, and `raw` return JSON objects because they are primarily automation interfaces.

## Raw API fallback

New Plane/fork endpoints do not require a CLI release first:

```bash
plane raw GET /api/v1/workspaces/
plane raw GET /api/v1/workspaces/tech/projects/ --query per_page=100
plane raw POST /api/v1/workspaces/tech/wiki/pages/ \
  --data '{"name":"Agent page"}'
```

## Safety model

- Read/report commands may fan out across every workspace visible to an IAT.
- Mutating commands **never fan out**.
- If a token can access multiple workspaces, a write fails until `--workspace` (or a default workspace) resolves exactly one target.
- API secrets are never printed by config commands.
- Config file permissions are restricted to `0600`.

## Current scope

V0.1 prioritizes:

- service-token discovery
- cross-workspace reads
- work-item create/update
- Wiki read/write/lifecycle
- cross-workspace reports
- stable JSON/CSV output
- raw endpoint fallback

The implementation plan for broader CRUD, richer reports, release packaging, shell completion and agent integration is in [`docs/PLAN.md`](docs/PLAN.md).

## Development

```bash
make test
make check
make build
```

## License

MIT
