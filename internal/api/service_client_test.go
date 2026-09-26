package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClientAutoBindsWSATWorkspace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/auth/context/", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(AuthContext{
			PrincipalType: "service",
			ScopeLevel:    "workspace",
			IsService:     true,
			Workspace:     &WorkspaceSummary{ID: "ws-1", Name: "Engineering", Slug: "engineering"},
			Scopes:        []string{"projects:read"},
		}))
	}))
	defer server.Close()

	oldCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = oldCfg })
	config.Cfg.APIHost = server.URL
	config.Cfg.DefaultWorkspace = ""
	t.Setenv("PLANE_API_KEY", "plane_wsat_test")

	client, err := NewClient()
	require.NoError(t, err)
	assert.Equal(t, "engineering", client.Workspace)
}

func TestNewClientDoesNotAutoBindIAT(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/auth/context/", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(AuthContext{
			PrincipalType: "service",
			ScopeLevel:    "instance",
			IsService:     true,
			Workspace:     nil,
			Scopes:        []string{"workspaces:read", "projects:read"},
		}))
	}))
	defer server.Close()

	oldCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = oldCfg })
	config.Cfg.APIHost = server.URL
	config.Cfg.DefaultWorkspace = ""
	t.Setenv("PLANE_API_KEY", "plane_iat_test")

	client, err := NewClient()
	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "no workspace selected")
}
