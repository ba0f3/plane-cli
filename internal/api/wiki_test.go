package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListWikiPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/workspaces/engineering/wiki/pages/", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("archived"))
		assert.Equal(t, "2026-09-25T00:00:00Z", r.URL.Query().Get("updated_after"))
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{{"id": "page-1", "name": "Runbook"}},
		}))
	}))
	defer server.Close()

	client := &Client{HTTPClient: server.Client(), BaseURL: server.URL, APIKey: "test-token", Workspace: "engineering"}
	pages, err := client.ListWikiPages(true, "2026-09-25T00:00:00Z")
	require.NoError(t, err)
	require.Len(t, pages, 1)
	assert.Equal(t, "page-1", pages[0]["id"])
}

func TestCreateAndUpdateWikiPage(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/v1/workspaces/engineering/wiki/pages/", r.URL.Path)
			var body map[string]interface{}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "Runbook", body["name"])
			require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{"id": "page-1", "name": "Runbook"}))
		case 2:
			assert.Equal(t, http.MethodPatch, r.Method)
			assert.Equal(t, "/api/v1/workspaces/engineering/wiki/pages/page-1/", r.URL.Path)
			var body map[string]interface{}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "Updated Runbook", body["name"])
			require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{"id": "page-1", "name": "Updated Runbook"}))
		default:
			t.Fatalf("unexpected request %d", calls)
		}
	}))
	defer server.Close()

	client := &Client{HTTPClient: server.Client(), BaseURL: server.URL, APIKey: "test-token", Workspace: "engineering"}
	created, err := client.CreateWikiPage(map[string]interface{}{"name": "Runbook"})
	require.NoError(t, err)
	assert.Equal(t, "page-1", created["id"])

	updated, err := client.UpdateWikiPage("page-1", map[string]interface{}{"name": "Updated Runbook"})
	require.NoError(t, err)
	assert.Equal(t, "Updated Runbook", updated["name"])
	assert.Equal(t, 2, calls)
}

func TestWikiPageAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/workspaces/engineering/wiki/pages/page-1/archive/", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{"id": "page-1", "archived_at": "2026-09-25T00:00:00Z"}))
	}))
	defer server.Close()

	client := &Client{HTTPClient: server.Client(), BaseURL: server.URL, APIKey: "test-token", Workspace: "engineering"}
	page, err := client.WikiPageAction("page-1", "archive")
	require.NoError(t, err)
	assert.Equal(t, "page-1", page["id"])

	_, err = client.WikiPageAction("page-1", "delete")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported wiki action")
}

func TestWikiRequiresWorkspace(t *testing.T) {
	client := &Client{}
	_, err := client.ListWikiPages(false, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workspace is required")
}
