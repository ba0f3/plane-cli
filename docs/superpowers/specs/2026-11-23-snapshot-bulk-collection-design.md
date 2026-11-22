# One-shot work-item snapshot for agent workload reports

Date: 2026-11-23

## Problem

When an agent is asked for a work or workload report, the instructions injected
into `AGENTS.md` by `plane-cli inject` describe `issue list` followed by
`issue view <id>` for detail. Agents follow that pattern and issue one CLI
invocation per work item. This is slow, consumes an LLM turn and tokens per
item, and creates avoidable API traffic that hits rate limits.

The repository already contains `skills/plane/SKILL.md`, which describes the
correct approach (collect one reusable snapshot, calculate locally, enrich only
a small evidence set). It is not reachable by every agent: it is only shipped as
a zip via `make skill-zip`, while the injected `AGENTS.md` and the generated
`plane-cli skills` output describe a per-item workflow.

## Goal

Give agents one command that collects a complete, detailed work-item dataset in
a single CLI process, and make every instruction surface point agents to that
command instead of per-item reads.

## Decisions (from design discussion)

- **Layer**: change both the CLI and the agent instructions.
- **Detail level**: full raw work-item fields with `expand=assignees,state,labels`.
  Do not pass records through the `plane.Issue` presentation struct.
- **Scope**: default to one workspace (via the existing global `--workspace`),
  with `--all-workspaces` for IAT and repeatable `--only-project` selection.
  The existing global `--project` is also honoured as a single selector;
  `--only-project` cannot be named `--project` because that name is already a
  persistent root flag.
- **Time range**: supported as a *local* filter before writing the snapshot
  (`--since`, `--until`, `--date-field created|updated`). No server-side date
  filter: it is unverified on this endpoint and would risk losing unchanged
  open work. The parser lives in `internal/timeutil` and is shared with
  `cmd/report` rather than copied.
- **Per-item view hint**: `issue view` itself emits a stderr hint when an agent
  repeatedly views items, so the N+1 pattern is discouraged even when the agent
  never reads the instruction surfaces.
- **Implementation**: a native Go collector plus a top-level `snapshot`
  command. No Python collector script, no `report export` variant (which would
  inherit the 500-item ceiling and the typed projection).

## Non-goals

- No edit, comment, notification, or publication operations. Collection is
  read-only.
- No per-item activity/comment/worklog enrichment in `snapshot`. The skill keeps
  bounded `raw GET` enrichment for a small evidence set; that stays as-is.
- No change to existing `issue list`, `issue view`, `report`, or `digest`
  behaviour.
- No server-side date filtering or new API client methods beyond reusing
  `GetRaw`.

## Design

### `internal/snapshot/collector.go` (new package)

Single-purpose package, unit-testable with `httptest`.

```go
type Record struct {
    Workspace         string          `json:"workspace"`
    ProjectID         string          `json:"project_id"`
    ProjectIdentifier string          `json:"project_identifier"`
    Item              json.RawMessage `json:"item"`
}

type ProjectCoverage struct {
    Workspace  string `json:"workspace"`
    ProjectID  string `json:"project_id"`
    Items      int    `json:"items"`
    Retained   int    `json:"retained"`
    Pages      int    `json:"pages"`
    Duplicates int    `json:"duplicates"`
}

type Scope struct {
    Workspace      string   `json:"workspace,omitempty"`
    AllWorkspaces  bool     `json:"all_workspaces"`
    Projects       []string `json:"projects,omitempty"`
    Expand         string   `json:"expand"`
    DateField      string   `json:"date_field,omitempty"`
    Since          string   `json:"since,omitempty"`
    Until          string   `json:"until,omitempty"`
}

type Snapshot struct {
    StartedAt  string           `json:"started_at"`
    FinishedAt string           `json:"finished_at"`
    Scope      Scope            `json:"scope"`
    Coverage   []ProjectCoverage `json:"coverage"`
    Records    []Record         `json:"records"`
}

func Collect(scope Scope) (*Snapshot, error)
```

Collection algorithm, per selected project:

1. Build `GET /workspaces/{ws}/projects/{pid}/work-items/` with
   `expand=assignees,state,labels`, `per_page=100`, and a `cursor` on later
   pages. Use the existing `api.Client.GetRaw` so server fields are preserved.
2. Require `results` to be an array and `next_page_results` to be a boolean.
   An unknown schema is an error, not an empty result.
3. Append every item's raw JSON keyed by `workspace + project + item id`;
   count duplicate IDs.
4. Continue while `next_page_results` is true, using `next_cursor`. A missing,
   empty, or repeated cursor is an error. Safety cap: 10,000 pages.
5. After all projects succeed, apply the optional local date filter to
   `records`. `internal/snapshot/timefilter.go` calls `timeutil.ParseTime`, the
   shared parser extracted from `cmd/report` (inclusive bounds; date-only
   `until` means midnight at the start of that date; `Nd`, Go duration, and
   RFC3339 inputs accepted). `cmd/report` is migrated to the same helper so the
   two commands cannot drift; `cmd/digest` keeps its existing parser and is out
   of scope. Coverage reports both fetched (`items`) and `retained` counts per
   project.
6. Return the snapshot only if every project succeeded. Never return partial
   data as a complete snapshot.

Reliability rules:

- 401/403: fail immediately with a message about token scope.
- 429: bounded exponential backoff with jitter; honour `Retry-After` when
  parseable; never retry in parallel.
- Other 4xx/5xx: fail immediately.
- All fetch errors are reported with workspace/project context.
- The command writes the output file atomically (temp file + rename) and only
  after `Collect` succeeds.

Scope resolution reuses existing patterns from `report.collect()`:

- `--all-workspaces` uses `api.NewClientNoWorkspace()` and enumerates
  workspaces; otherwise the workspace comes from `--workspace` or config.
- Projects come from `ListProjects()` on a workspace-scoped client and are
  filtered by the repeatable `--only-project` selectors (ID, identifier, or
  name, case-insensitive, matching `report.projectMatches`). If none are
  given, the global `--project` is used as one selector **only when it was
  explicitly passed on the command line**. The configured
  `config.Cfg.DefaultProject` (config file / `PLANE_PROJECT`) is deliberately
  **not** inherited by `snapshot`: a default project must never silently
  narrow a workspace-scoped snapshot to one project, so omitting `--project`
  means "every project in scope".
- `--all-workspaces` is mutually exclusive with a selected workspace, i.e.
  with an explicit global `--workspace` **or** a configured workspace
  (`config.Cfg.DefaultWorkspace` / `PLANE_WORKSPACE`).

### `cmd/snapshot/snapshot.go` (new command)

Registered in `cmd/root.go`. Flags:

| Flag | Meaning |
| --- | --- |
| (global) `--workspace WS` | Workspace slug; overrides config. Do not redefine |
| `--all-workspaces` | Collect every visible workspace (IAT); rejected when a workspace is selected by flag, env, or config |
| `--only-project SELECTOR` | Repeatable project filter: ID, identifier, or name |
| (global) `--project` | Fallback single selector when `--only-project` is empty. Honoured only when explicitly passed; the configured default project is ignored |
| `--expand LIST` | Default `assignees,state,labels` |
| `--out PATH` | Default `./plane-snapshot.json` |
| `--since`, `--until`, `--date-field` | Local record filter; same parser as `report` |
| `-o, --output json\|yaml` | Format of the small stdout summary only |
| `--quiet` | Suppress stdout summary |

Behaviour:

- The bulk records are written only to `--out`. They are never printed to
  stdout/stderr, keeping them out of model context.
- Stdout summary: `file`, `started_at`, `finished_at`, `unique_items`, and
  `coverage`. Collection errors fail the command; no summary or output file is
  produced for a failed collection.
- JSON/YAML output uses the existing `internal/output` helpers for the summary.

### Agent instruction surfaces

All three surfaces must state the same rule: for workload/report requests,
collect one snapshot and calculate locally; do not call `issue view` once per
task.

1. `skills/plane/SKILL.md`
   - Section 2: point the "team comparison / deep analysis" row at
     `plane-cli snapshot` instead of a generic bulk snapshot.
   - Section 3: replace the Python collector with one `snapshot` invocation
     plus local Python/jq aggregation; document the raw schema and coverage.
   - Add an explicit anti-pattern note about `issue list` + per-item
     `issue view`.
   - Keep section 6 (bounded raw enrichment of 5-10 evidence items).
2. `cmd/inject/inject.go` (`generateContent`)
   - Add a "Reporting / bulk analysis" subsection with the rule and a
     `plane-cli snapshot` example. The existing issue command help stays, but
     the new subsection overrides it for report requests.
3. `cmd/skills.go` (`renderSkill`)
   - Add an agent operating rule about one-shot collection, and replace the
     reporting example in "Common agent workflows" with `snapshot`. Because
     this reference is generated from the command tree, the new command also
     appears automatically.

### Repetitive `issue view` hint

A new `internal/viewhint` package records one event per successful `issue view`
invocation in an append-only JSONL file under
`os.UserCacheDir()/plane-cli/view-hints.jsonl`. Each line carries a key
(`workspace/project`), an event (`view` or `warn`), and a UTC timestamp. The
package returns whether to emit a warning: warn at the **3rd view within 5
minutes** for a key, and at most **once per 5-minute window** (a `warn` event
suppresses further warnings until the window passes). The cache file contains
no credentials; failures to read or write it are best-effort and must never
fail the `issue view` command.

The cache file is bounded: once it grows past 500 lines it is rewritten
atomically (temp file + rename in the same directory) with only the events
still inside the current 5-minute window. `warn` events are kept so the
once-per-window suppression survives the rewrite. Pruning is best-effort.

`runView` in `cmd/issue/issue.go` calls the checker with the configured
workspace/project **after** the issue was resolved successfully and before
the formatter prints, so failed views do not count towards the threshold, and
writes the fixed warning to `cmd.ErrOrStderr()`:

```
plane-cli: repeated issue view calls detected (3 in 5m); use plane-cli snapshot to collect all items in one invocation instead of viewing each task.
```

The warning goes to stderr only, so JSON/table output on stdout stays clean.
There is no disable flag or environment variable. Tests inject the cache
directory through a package variable and control time explicitly; they cover
threshold, once-per-window suppression, key isolation, and best-effort failure.

### Testing

- `internal/snapshot/collector_test.go` with `httptest`: multi-page cursor walk,
  dedupe, malformed pagination schema, 401 failure, 429 retry, local time
  filter, coverage counts, scope resolution across one and several workspaces,
  project selector matching.
- `internal/timeutil/timeutil_test.go`: parsing table (Nd, duration, RFC3339,
  date-only, invalid).
- `internal/viewhint/viewhint_test.go`: threshold, once-per-window suppression,
  key isolation, cache failure is non-fatal.
- `cmd/snapshot/snapshot_test.go`: mutually exclusive flags, output file
  creation, summary format, no bulk records on stdout.
- Verify with `go test ./...` and `make check`. Live API tests are not required
  for this change.

## Risks and mitigations

- **Unknown endpoint response shape**: the collector validates the schema and
  fails loudly instead of silently returning an incomplete snapshot.
- **Large workspaces**: cursor pagination has no item ceiling but does have a
  10,000-page safety cap; the local date filter reduces output size when
  needed.
- **Instruction drift across three files**: a single design rule is documented
  in the spec and repeated in all three surfaces; tests cover the generated
  surfaces.
