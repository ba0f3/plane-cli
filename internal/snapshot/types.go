package snapshot

import "encoding/json"

// Record keeps the server's work-item representation verbatim so callers see
// fields that the presentation layer of `issue list` does not project.
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
	Workspace     string   `json:"workspace,omitempty"`
	AllWorkspaces bool     `json:"all_workspaces"`
	Projects      []string `json:"projects,omitempty"`
	Expand        string   `json:"expand"`
	DateField     string   `json:"date_field,omitempty"`
	Since         string   `json:"since,omitempty"`
	Until         string   `json:"until,omitempty"`
}

type Snapshot struct {
	StartedAt  string            `json:"started_at"`
	FinishedAt string            `json:"finished_at"`
	Scope      Scope             `json:"scope"`
	Coverage   []ProjectCoverage `json:"coverage"`
	Records    []Record          `json:"records"`
}

// Summary is the only snapshot information printed to stdout.
type Summary struct {
	File        string            `json:"file"`
	StartedAt   string            `json:"started_at"`
	FinishedAt  string            `json:"finished_at"`
	UniqueItems int               `json:"unique_items"`
	Coverage    []ProjectCoverage `json:"coverage"`
}
