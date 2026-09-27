package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ba0f3/plane-cli/pkg/plane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientListProjectsWalksCursorPagination(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assert.Equal(t, "/api/v1/workspaces/test-workspace/projects/", r.URL.Path)
		assert.Equal(t, "100", r.URL.Query().Get("per_page"))

		cursor := r.URL.Query().Get("cursor")
		var response Response
		switch cursor {
		case "":
			response = Response{
				Pagination: Pagination{NextCursor: "next-1", NextPageResults: true},
				Results:    mustMarshal(t, []plane.Project{{ID: "p1", Identifier: "P1", Name: "Project 1"}}),
			}
		case "next-1":
			response = Response{
				Results: mustMarshal(t, []plane.Project{{ID: "p2", Identifier: "P2", Name: "Project 2"}}),
			}
		default:
			t.Fatalf("unexpected cursor %q", cursor)
		}

		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer server.Close()

	client := &Client{
		HTTPClient: server.Client(),
		BaseURL:    server.URL,
		APIKey:     "test-api-key",
		Workspace:  "test-workspace",
	}

	projects, err := client.ListProjects()
	require.NoError(t, err)
	assert.Equal(t, 2, requests)
	require.Len(t, projects, 2)
	assert.Equal(t, "p1", projects[0].ID)
	assert.Equal(t, "p2", projects[1].ID)
}

func TestClientListProjectsRejectsRepeatedCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := Response{
			Pagination: Pagination{NextCursor: "loop", NextPageResults: true},
			Results:    mustMarshal(t, []plane.Project{}),
		}
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer server.Close()

	client := &Client{
		HTTPClient: server.Client(),
		BaseURL:    server.URL,
		APIKey:     "test-api-key",
		Workspace:  "test-workspace",
	}

	_, err := client.ListProjects()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repeated cursor")
}
