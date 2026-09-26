package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAuthContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/auth/context/", r.URL.Path)
		assert.Equal(t, "test-token", r.Header.Get("X-API-Key"))
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"principal_type": "service",
			"scope_level":    "workspace",
			"is_service":     true,
			"workspace": map[string]interface{}{
				"id":   "ws-1",
				"name": "Engineering",
				"slug": "engineering",
			},
			"scopes": []string{"projects:read", "work_items:read"},
		}))
	}))
	defer server.Close()

	client := &Client{HTTPClient: server.Client(), BaseURL: server.URL, APIKey: "test-token"}
	ctx, err := client.GetAuthContext()
	require.NoError(t, err)
	assert.True(t, ctx.IsService)
	assert.Equal(t, "service", ctx.PrincipalType)
	assert.Equal(t, "workspace", ctx.ScopeLevel)
	require.NotNil(t, ctx.Workspace)
	assert.Equal(t, "engineering", ctx.Workspace.Slug)
	assert.Equal(t, []string{"projects:read", "work_items:read"}, ctx.Scopes)
}

func TestListWorkspacesSupportsDirectAndWrappedResponses(t *testing.T) {
	tests := []struct {
		name string
		body interface{}
	}{
		{
			name: "direct",
			body: []WorkspaceSummary{{ID: "ws-1", Name: "Engineering", Slug: "engineering"}},
		},
		{
			name: "wrapped",
			body: map[string]interface{}{
				"results": []WorkspaceSummary{{ID: "ws-1", Name: "Engineering", Slug: "engineering"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/v1/workspaces/", r.URL.Path)
				require.NoError(t, json.NewEncoder(w).Encode(tt.body))
			}))
			defer server.Close()

			client := &Client{HTTPClient: server.Client(), BaseURL: server.URL, APIKey: "test-token"}
			workspaces, err := client.ListWorkspaces()
			require.NoError(t, err)
			require.Len(t, workspaces, 1)
			assert.Equal(t, "engineering", workspaces[0].Slug)
		})
	}
}
