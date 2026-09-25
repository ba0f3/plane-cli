# Plane CLI — implementation plan

Status: V0.1 implemented on `feat/go-plane-cli-v0.1`.

## 1. Product goal

Provide one small Go binary that can replace most MCP usage for deterministic Plane management/reporting and can be invoked by humans, cron jobs, goclaw, OpenCode/Codex, or any agent that can execute a process.

The CLI must treat Plane as a hierarchy:

```text
Instance
  -> Workspace(s)
     -> Project(s)
        -> Work items / cycles / modules / states / labels
     -> Wiki
```

`workspace_slug` is a resource selector, not a mandatory identity parameter.

## 2. Authentication model

### PAT

- identity: user
- workspace discovery returns workspaces visible to the user
- optional default workspace remains useful

### WSAT

- identity: service principal
- `/api/v1/auth/context/` reports `scope_level=workspace`
- workspace discovery returns the single bound workspace
- commands can omit `--workspace`

### IAT

- identity: service principal
- `/api/v1/auth/context/` reports `scope_level=instance`
- workspace discovery returns all active workspaces
- read/report commands default to all workspaces
- write commands require exactly one workspace

This is the main behavioral difference from Plane MCP.

## 3. V0.1 command surface

```text
plane configure
plane auth context
plane workspace list
plane project list|show
plane work-item list|show|create|update
plane cycle list
plane module list
plane state list
plane label list
plane member list
plane wiki list|show|create|update|archive|unarchive|lock|unlock
plane report summary|workload|digest
plane raw METHOD PATH
```

Global:

```text
--base-url
--token
--workspace / -w
--config
--output / -o  table|json|csv
```

Precedence:

```text
CLI > environment > config file
```

## 4. V0.1 report semantics

### `report summary`

Granularity: workspace + project.

Metrics:

- total work items
- active
- completed/cancelled
- completion percentage
- overdue open items
- unassigned
- estimate points

### `report workload`

Metrics:

- count
- points
- workload share percentage

Group modes:

- assignee
- workspace-assignee
- project-assignee

Filters apply before aggregation.

### `report digest`

Default window: previous 24h based on `updated_at`.

Can be changed to weekly or arbitrary windows:

```bash
plane report digest --since 7d
plane report digest --since 2026-09-01 --until 2026-09-08
```

The output is intentionally machine-friendly so another LLM/agent can summarize it without needing Plane MCP.

## 5. Architecture

```text
cmd/plane
  -> internal/cli       command parsing + safety policy
  -> internal/config    config/env precedence, secret file permissions
  -> internal/api       HTTP, authentication header, pagination
  -> internal/report    cross-workspace collection + normalization
  -> internal/output    table/json/csv
```

### Design decisions

1. Raw REST instead of a Go Plane SDK.
   - Fork APIs can ship independently of SDKs.
   - `raw` provides immediate forward compatibility.
2. Standard library only.
   - Easy static builds.
   - No module supply-chain dependency for an administrative binary.
3. Data normalization at the report boundary.
   - Plane response shapes can differ across CE/release versions.
   - Reporting code tolerates expanded objects or raw IDs.
4. Mutations do not fan out.
   - Prevent an IAT from accidentally changing every workspace.

## 6. P1 — management parity

Add full CRUD where public APIs support it:

- projects: create/update/delete
- cycles: create/update/delete, add/remove work items
- modules: create/update/delete, add/remove work items
- states: create/update/delete
- labels: create/update/delete
- comments: list/create/update/delete
- intake: list/create/accept/decline/delete
- work items: delete, relationships, parents/sub-items, comments

Destructive commands should require `--yes` in interactive use and support `--force` only when semantics are explicit.

## 7. P1 — report expansion

### Leadership / workspace overview

- work item distribution by state/priority/label/module/cycle
- workload by assignee and project/product label
- throughput per week/month
- created vs completed trend
- overdue trend
- cycle completion / spillover
- unplanned work ratio
- lead time / cycle time when timestamps are available

### KPI/export

- `report workload --group-by label-assignee`
- contribution matrix: assignee x project/label
- CSV designed for Sheets/BI
- optional Markdown digest for Telegram/Slack

### Time tracking

If the fork exposes activity/time-tracking data, add:

```text
--metric count|points|duration
```

Do not infer duration from start/target dates as "time spent".

## 8. P1 — agent UX

- `--fields` to reduce JSON size/tokens
- `--jq` is intentionally not embedded; pipe JSON to external `jq`
- deterministic exit codes
- `--quiet`
- `--no-header`
- shell completion
- a small `skills/SKILL.md` teaching agents how to use the CLI
- report presets stored in config, e.g. `weekly-tech`, `daily-company`

## 9. P1 — performance

Current V0.1 fans out API calls per project with bounded concurrency.

Next optimizations:

- workspace member cache per run
- conditional requests / ETags if Plane supports them
- request retry for 429/5xx with jitter
- configurable concurrency
- server-side filters whenever public API semantics are stable
- streaming JSON for very large instances

## 10. P1 — distribution

- GitHub Actions: test, vet, build
- release binaries: linux amd64/arm64, darwin amd64/arm64, windows amd64
- SHA256 checksums
- optional Homebrew tap
- version injected by `-ldflags`

## 11. Acceptance criteria

V0.1 is acceptable when:

1. `go test ./...` passes with no network dependencies.
2. IAT can discover >1 workspace without a workspace argument.
3. cross-workspace reports aggregate all discovered workspace/project data.
4. write with IAT and no selected workspace is rejected before mutation.
5. work item create/update resolves friendly project/member/state/label selectors.
6. Wiki `read/write/lifecycle` works with the fork's service-token API.
7. table/json/csv are stable enough for humans/scripts.
8. unsupported/new APIs remain reachable through `plane raw`.
