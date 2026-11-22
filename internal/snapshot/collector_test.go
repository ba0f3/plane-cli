package snapshot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ba0f3/plane-cli/internal/api"
	"github.com/ba0f3/plane-cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClient(t *testing.T, handler http.Handler) (*api.Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &api.Client{
		HTTPClient: server.Client(), BaseURL: server.URL,
		APIKey: "test-key", Workspace: "ws",
	}, server
}

func writeProjects(t *testing.T, w http.ResponseWriter, projects ...map[string]string) {
	t.Helper()
	results := make([]json.RawMessage, 0, len(projects))
	for _, project := range projects {
		encoded, err := json.Marshal(project)
		require.NoError(t, err)
		results = append(results, encoded)
	}
	require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
		"results": results, "next_page_results": false,
	}))
}

func TestResolveProjectsSingleWorkspace(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/workspaces/ws/projects/", r.URL.Path)
		writeProjects(t, w, map[string]string{"id": "p1", "identifier": "GAME", "name": "Game"})
	}))

	refs, err := resolveProjects(Scope{Workspace: "ws"}, client)
	require.NoError(t, err)
	assert.Equal(t, []projectRef{{Workspace: "ws", ID: "p1", Identifier: "GAME"}}, refs)
}

func TestResolveProjectsFiltersBySelector(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProjects(t, w,
			map[string]string{"id": "p1", "identifier": "GAME", "name": "Game"},
			map[string]string{"id": "p2", "identifier": "WEB", "name": "Web"},
		)
	}))

	refs, err := resolveProjects(Scope{Workspace: "ws", Projects: []string{"game"}}, client)
	require.NoError(t, err)
	assert.Equal(t, []projectRef{{Workspace: "ws", ID: "p1", Identifier: "GAME"}}, refs)
}

func TestResolveProjectsAllWorkspaces(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/workspaces/":
			require.NoError(t, json.NewEncoder(w).Encode([]map[string]string{
				{"slug": "ws-a", "name": "A"}, {"slug": "ws-b", "name": "B"},
			}))
		case "/api/v1/workspaces/ws-a/projects/":
			writeProjects(t, w, map[string]string{"id": "p1", "identifier": "GAME"})
		case "/api/v1/workspaces/ws-b/projects/":
			writeProjects(t, w, map[string]string{"id": "p2", "identifier": "WEB"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))

	refs, err := resolveProjects(Scope{AllWorkspaces: true}, client)
	require.NoError(t, err)
	assert.Equal(t, []projectRef{
		{Workspace: "ws-a", ID: "p1", Identifier: "GAME"},
		{Workspace: "ws-b", ID: "p2", Identifier: "WEB"},
	}, refs)
}

func TestResolveProjectsRejectsAllWorkspacesWithWorkspace(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no HTTP request expected")
	}))

	_, err := resolveProjects(Scope{Workspace: "ws", AllWorkspaces: true}, client)
	assert.ErrorContains(t, err, "--all-workspaces")
}

func pageItem(id string) string {
	return fmt.Sprintf(`{"id":%q,"created_at":"2026-11-01T00:00:00Z","updated_at":"2026-11-02T00:00:00Z"}`, id)
}

func TestCollectProjectWalksCursorAndDedupes(t *testing.T) {
	var requests int
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/workspaces/ws/projects/p1/work-items/", r.URL.Path)
		assert.Equal(t, "assignees,state,labels", r.URL.Query().Get("expand"))
		assert.Equal(t, "100", r.URL.Query().Get("per_page"))
		requests++
		switch r.URL.Query().Get("cursor") {
		case "":
			fmt.Fprintf(w, `{"results":[%s],"next_page_results":true,"next_cursor":"c1"}`, pageItem("a"))
		case "c1":
			fmt.Fprintf(w, `{"results":[%s,%s],"next_page_results":false}`, pageItem("a"), pageItem("b"))
		default:
			t.Fatal("unexpected cursor")
		}
	}))

	byID := map[string]Record{}
	cov, err := collectProject(client, projectRef{Workspace: "ws", ID: "p1", Identifier: "GAME"},
		"assignees,state,labels", byID, func(time.Duration) {})
	require.NoError(t, err)
	assert.Equal(t, 2, requests)
	assert.Equal(t, ProjectCoverage{
		Workspace: "ws", ProjectID: "p1", Items: 3, Retained: 2, Pages: 2, Duplicates: 1,
	}, cov)
	assert.Len(t, byID, 2)
}

func TestCollectProjectRejectsMalformedPagination(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"results":[%s]}`, pageItem("a"))
	}))

	_, err := collectProject(client, projectRef{Workspace: "ws", ID: "p1"}, "expand", map[string]Record{}, func(time.Duration) {})
	assert.ErrorContains(t, err, "next_page_results")
}

func TestCollectProjectRejectsRepeatedCursor(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"results":[%s],"next_page_results":true,"next_cursor":"c1"}`, pageItem("a"))
	}))

	_, err := collectProject(client, projectRef{Workspace: "ws", ID: "p1"}, "expand", map[string]Record{}, func(time.Duration) {})
	assert.ErrorContains(t, err, "repeated next cursor")
}

func TestFetchPageRetries429AndRespectsRetryAfter(t *testing.T) {
	var calls int
	var slept []time.Duration
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprintf(w, `{"results":[],"next_page_results":false}`)
	}))

	_, err := fetchPage(client, projectRef{Workspace: "ws", ID: "p1"}, "expand", "",
		func(d time.Duration) { slept = append(slept, d) })
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	assert.Equal(t, []time.Duration{2 * time.Second}, slept)
}

func TestFetchPageFailsOn401(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"denied"}`)
	}))

	_, err := fetchPage(client, projectRef{Workspace: "ws", ID: "p1"}, "expand", "", func(time.Duration) {})
	assert.ErrorContains(t, err, "token scope")
}

func TestCollectBuildsSnapshot(t *testing.T) {
	t.Setenv("PLANE_API_KEY", "test-key")
	originalHost, originalWorkspace := config.Cfg.APIHost, config.Cfg.DefaultWorkspace
	t.Cleanup(func() {
		config.Cfg.APIHost, config.Cfg.DefaultWorkspace = originalHost, originalWorkspace
	})

	client, server := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/workspaces/ws/projects/":
			writeProjects(t, w, map[string]string{"id": "p1", "identifier": "GAME"})
		case "/api/v1/workspaces/ws/projects/p1/work-items/":
			fmt.Fprintf(w, `{"results":[%s,%s],"next_page_results":false}`, pageItem("a"), pageItem("b"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	config.Cfg.APIHost, config.Cfg.DefaultWorkspace = server.URL, ""

	snapshot, err := Collect(Scope{Workspace: "ws", Expand: "assignees,state,labels"})
	require.NoError(t, err)
	assert.Len(t, snapshot.Records, 2)
	assert.Equal(t, 2, snapshot.Coverage[0].Items)
	assert.Equal(t, 2, snapshot.Coverage[0].Retained)
	assert.NotEmpty(t, snapshot.StartedAt)
	assert.NotEmpty(t, snapshot.FinishedAt)
	assert.NotNil(t, client)
}
