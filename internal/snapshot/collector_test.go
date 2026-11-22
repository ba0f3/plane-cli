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

	refs, err := resolveProjects(Scope{Workspace: "ws"}, client, func(time.Duration) {})
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

	refs, err := resolveProjects(Scope{Workspace: "ws", Projects: []string{"game"}}, client, func(time.Duration) {})
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

	refs, err := resolveProjects(Scope{AllWorkspaces: true}, client, func(time.Duration) {})
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

	_, err := resolveProjects(Scope{Workspace: "ws", AllWorkspaces: true}, client, func(time.Duration) {})
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
	require.Len(t, slept, 1)
	// A 2s Retry-After is jittered within [1s, 2s].
	assert.GreaterOrEqual(t, slept[0], time.Second)
	assert.LessOrEqual(t, slept[0], 2*time.Second)
}

func TestFetchPageRejectsMissingResults(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"next_page_results":false}`)
	}))

	_, err := fetchPage(client, projectRef{Workspace: "ws", ID: "p1"}, "expand", "", func(time.Duration) {})
	assert.ErrorContains(t, err, "missing results")
}

func TestFetchPageRejectsNullResults(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results":null,"next_page_results":false}`)
	}))

	_, err := fetchPage(client, projectRef{Workspace: "ws", ID: "p1"}, "expand", "", func(time.Duration) {})
	assert.ErrorContains(t, err, "results must be an array")
}

func TestRetryDelayWithJitterStaysWithinBounds(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		attempt int
		base    time.Duration
	}{
		{name: "retry-after seconds", header: "2", attempt: 0, base: 2 * time.Second},
		{name: "retry-after capped at 30s", header: "120", attempt: 0, base: 30 * time.Second},
		{name: "exponential first retry", attempt: 0, base: time.Second},
		{name: "exponential third retry", attempt: 2, base: 4 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < 100; i++ {
				delay := retryDelayWithJitter(tt.header, tt.attempt)
				assert.GreaterOrEqual(t, delay, tt.base/2)
				assert.LessOrEqual(t, delay, tt.base)
			}
		})
	}
}

func TestResolveProjectsRetriesDiscovery429(t *testing.T) {
	var calls int
	var slept []time.Duration
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writeProjects(t, w, map[string]string{"id": "p1", "identifier": "GAME"})
	}))

	refs, err := resolveProjects(Scope{Workspace: "ws"}, client,
		func(d time.Duration) { slept = append(slept, d) })
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	require.Len(t, slept, 1)
	assert.GreaterOrEqual(t, slept[0], 500*time.Millisecond)
	assert.LessOrEqual(t, slept[0], time.Second)
	assert.Equal(t, []projectRef{{Workspace: "ws", ID: "p1", Identifier: "GAME"}}, refs)
}

func TestResolveProjectsReportsTokenScopeOn401(t *testing.T) {
	client, _ := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"denied"}`)
	}))

	_, err := resolveProjects(Scope{Workspace: "ws"}, client, func(time.Duration) {})
	assert.ErrorContains(t, err, "workspace ws: list projects")
	assert.ErrorContains(t, err, "token scope")
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

	_, server := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}

func TestCollectRejectsUnmatchedSelector(t *testing.T) {
	t.Setenv("PLANE_API_KEY", "test-key")
	originalHost, originalWorkspace := config.Cfg.APIHost, config.Cfg.DefaultWorkspace
	t.Cleanup(func() {
		config.Cfg.APIHost, config.Cfg.DefaultWorkspace = originalHost, originalWorkspace
	})

	_, server := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workspaces/ws/projects/" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		writeProjects(t, w, map[string]string{"id": "p1", "identifier": "GAME"})
	}))
	config.Cfg.APIHost, config.Cfg.DefaultWorkspace = server.URL, ""

	_, err := Collect(Scope{Workspace: "ws", Projects: []string{"missing"}})
	assert.ErrorContains(t, err, "no projects matched")
}
