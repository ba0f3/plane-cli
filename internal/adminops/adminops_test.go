package adminops

import (
	"testing"

	"github.com/ba0f3/plane-cli/pkg/plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMemberInventoryDeduplicatesAcrossWorkspaces(t *testing.T) {
	input := map[string][]plane.User{
		"design":      {{ID: "u1", Email: "alice@example.com", DisplayName: "Alice", Role: 15}},
		"engineering": {{ID: "u1", Email: "alice@example.com", DisplayName: "Alice", Role: 20}, {ID: "u2", Email: "bob@example.com", DisplayName: "Bob"}},
	}

	rows := BuildMemberInventory(input, "")
	require.Len(t, rows, 2)
	assert.Equal(t, "alice@example.com", rows[0].Email)
	assert.Equal(t, 2, rows[0].WorkspaceCount)
	assert.Len(t, rows[0].Memberships, 2)
}

func TestBuildMemberInventoryFilters(t *testing.T) {
	input := map[string][]plane.User{
		"design": {{ID: "u1", Email: "alice@example.com", DisplayName: "Alice"}, {ID: "u2", Email: "bob@example.com", DisplayName: "Bob"}},
	}

	rows := BuildMemberInventory(input, "BOB@EXAMPLE")
	require.Len(t, rows, 1)
	assert.Equal(t, "u2", rows[0].ID)
}

func TestAuditFindsProjectMemberOutsideWorkspace(t *testing.T) {
	snapshots := []WorkspaceSnapshot{{
		Workspace: "design",
		Members:   []plane.User{{ID: "u1", Email: "alice@example.com"}},
		Projects: []ProjectSnapshot{{
			Project: plane.Project{ID: "p1", Identifier: "DES"},
			Members: []plane.User{{ID: "u2", Email: "bob@example.com"}},
		}},
	}}

	result := Audit(snapshots)
	assert.Equal(t, 1, result.Summary.Workspaces)
	assert.Equal(t, 1, result.Summary.Projects)
	assert.Equal(t, 1, result.Summary.Members)
	assert.Equal(t, 1, result.Summary.Warnings)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, "project_member_not_in_workspace", result.Findings[0].Code)
}

func TestAuditReportsUnreadableEndpoints(t *testing.T) {
	result := Audit([]WorkspaceSnapshot{{Workspace: "broken", Error: "403 forbidden"}})
	assert.Equal(t, 1, result.Summary.Errors)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, "workspace_unreadable", result.Findings[0].Code)
}

func TestResolveProjects(t *testing.T) {
	projects := []plane.Project{
		{ID: "p1", Identifier: "APP", Name: "App"},
		{ID: "p2", Identifier: "WEB", Name: "Website"},
	}

	plan := ResolveProjects(projects, []string{"app", "Website", "missing", "p1"})
	require.Len(t, plan.Projects, 2)
	assert.Equal(t, []string{"missing"}, plan.Missing)
}
