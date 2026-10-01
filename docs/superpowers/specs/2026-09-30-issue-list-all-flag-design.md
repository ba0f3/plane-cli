# `--all` flag for issue listing commands

Date: 2026-09-30

## Problem

`plane-cli issue list` defaults to `--limit 20`. Plane paginates work-item
results, so any project with more than 20 issues silently hides work items from
the user. Sub-issue behaviour makes this more visible: the Plane API returns
parent and child work items interleaved in one flat result set, so truncating
at 20 can drop a sub-issue while keeping its parent, which reads as data loss.

`plane-cli issue search` and `plane-cli issue list --all-projects` have their
own, separate truncation paths, so the fix should cover all three listing
surfaces consistently.

## Goal

Add an `--all` boolean flag that disables result truncation and fetches the
complete result set, bounded by a hard limit of 500 issues per invocation.

## Non-goals

- No `--parent <uuid>` filter and no `--offset` flag.
- No parent/child tree rendering and no new `PARENT` output column. The API
  already returns `parent` on each work item; adding display for it is a
  separate concern.
- No change to `--limit` semantics. `--limit` keeps its current meaning for
  every invocation that does not pass `--all`.
- No changes to `internal/api.ListAllIssues`, which is used by `report` and
  `digest` and must keep its unbounded cursor-walking behaviour.

## Design

### API layer

Add to `internal/api/issues.go`:

```go
// MaxAllIssues is the hard ceiling applied when fetching an unbounded
// result set. Exceeding it is an error, not a silent truncation.
const MaxAllIssues = 500

// ListAllIssuesPaged walks offset pagination for one project and returns every
// matching work item matching opts' State and Assignee filters. It errors if
// the result set exceeds max.
func (c *Client) ListAllIssuesPaged(projectID string, opts IssueListOptions, max int) ([]plane.Issue, error)
```

`ListAllIssuesPaged` applies `IssueListOptions.State` and `.Assignee` as query
filters and ignores `.Limit`, because the caller has already opted out of
truncation. It requests a fixed page size of 100, which keeps a `--all`
invocation to five requests at the 500 ceiling.

Loop control:

- Terminate when a page returns zero results, or when the running total reaches
  `max`, or when a page returns fewer items than the requested page size (the
  last page).
- If the result set is strictly larger than `max`, return an error naming the
  ceiling and suggesting `--limit`. Partial results are not returned, because a
  truncated list that looks complete is worse than an error.

Offset pagination is used rather than cursor pagination deliberately.
`ListIssues` already exercises `limit`/`offset` together with the `state` and
`assignee` query parameters, so filters are known to compose correctly.
`ListAllIssues` uses cursor pagination but carries no filters, and that
combination (cursor + `state`/`assignee`) is untested against this endpoint; a
server that silently dropped the filter under cursor would return a plausible
but wrong result set.

`SearchIssues` is unchanged. Its endpoint returns the full match set in a
single response and has no pagination parameters, so `--all` only needs to
enforce the ceiling on the result it already returns.

### Command layer

`internal/api` owns fetching and the ceiling. `cmd/issue` owns flag parsing and
translating the ceiling error into user-facing guidance.

`cmd/issue/issue.go`:

- Register `--all` on `listCmd` and on `searchCmd`.
- `runList`: when `listAll` is set, call `ListAllIssuesPaged` with
  `IssueListOptions{State, Assignee}`. Otherwise the existing single
  `ListIssues` call is untouched. `--limit` is not forwarded, since `--all`
  supersedes it; the two flags are not mutually exclusive and `--all` wins.
- `runSearch`: when `listAll` is set and `len(issues) > MaxAllIssues`, return
  the same ceiling error. No fetch change.

`cmd/issue/workspace_list.go`:

- `--all` is inherited automatically: `--all-projects` shares `listCmd`.
- `runWorkspaceList` stops applying the `perPage` truncation when `listAll` is
  set, and instead enforces the ceiling across the aggregated result.

`collectWorkspaceIssues` currently calls the unbounded `ListAllIssues` per
project and takes `stateID`/`assigneeID` as plain function arguments.

Its signature gains an `all bool` parameter. When `all` is false it behaves
exactly as today: unbounded `ListAllIssues` plus client-side filtering via
`workspaceIssueMatches`, then `perPage` truncation.

When `all` is true it instead calls `ListAllIssuesPaged` with a descending
remaining budget — `MaxAllIssues` minus what has already been collected — so
the ceiling applies to the aggregate across projects rather than per project.
The final project, if it overruns its remaining budget, produces the ceiling
error. The existing `workspaceIssueMatches` filter still applies afterwards,
since a server-side filter and a client-side filter on the same two fields
agree.

### Output

Unchanged. The six columns `ID, #, TITLE, STATE, PRIORITY, ASSIGNEE` and their
JSON shape stay exactly as they are, so `--output json` consumers are
unaffected. `--all` changes how many rows arrive, not what a row contains.

### Errors

One error shape, produced in `cmd/issue`, worded around the ceiling:

```
project has more than 500 issues; use --limit to page through results
```

The workspace path reports the aggregate count rather than a single project's.

### Testing

Unit tests in `cmd/issue` using an `httptest` server, matching the existing
style in `internal/api/client_test.go`:

- `--all` issues exactly one request when the first page is short.
- `--all` pages until the ceiling when every page is full.
- `--all` errors when the result set exceeds 500.
- `--all` with `--state`/`--assignee` forwards those query parameters on every
  page.
- Without `--all`, `issue list` still issues exactly one request and still
  honours `--limit`.
- `--all` on `search` errors above the ceiling and passes below it.
- `--all --all-projects` enforces the ceiling across multiple projects.

Integration tests, tagged `integration` and serialized per `AGENTS.md`:

- `issue list --all` on a project with more than 20 issues returns more than 20.
