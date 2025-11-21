package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractToolHiveMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		server   upstreamv0.ServerJSON
		expected map[string]any
	}{
		{
			name: "with toolhive metadata",
			server: upstreamv0.ServerJSON{
				Meta: &upstreamv0.ServerMeta{
					PublisherProvided: map[string]any{
						"provider": map[string]any{
							"toolhive": map[string]any{
								"tier": "Community",
							},
						},
					},
				},
			},
			expected: map[string]any{
				"tier": "Community",
			},
		},
		{
			name:     "without metadata",
			server:   upstreamv0.ServerJSON{},
			expected: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := extractToolHiveMetadata(tt.server)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractStars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		server   upstreamv0.ServerJSON
		expected int64
	}{
		{
			name: "with stars",
			server: upstreamv0.ServerJSON{
				Meta: &upstreamv0.ServerMeta{
					PublisherProvided: map[string]any{
						"provider": map[string]any{
							"toolhive": map[string]any{
								"metadata": map[string]any{
									"stars": float64(150),
								},
							},
						},
					},
				},
			},
			expected: 150,
		},
		{
			name:     "without stars",
			server:   upstreamv0.ServerJSON{},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := extractStars(tt.server)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandleCompareServers(t *testing.T) {
	t.Parallel()

	server1 := upstreamv0.ServerJSON{
		Name:        "io.test/server1",
		Description: "Server 1 description",
		Version:     "1.0.0",
		Meta: &upstreamv0.ServerMeta{
			PublisherProvided: map[string]any{
				"provider": map[string]any{
					"toolhive": map[string]any{
						"metadata": map[string]any{
							"stars": float64(100),
							"pulls": float64(500),
						},
						"tier":      "Community",
						"status":    "Active",
						"transport": "stdio",
					},
				},
			},
		},
	}

	server2 := upstreamv0.ServerJSON{
		Name:        "io.test/server2",
		Description: "Server 2 description",
		Version:     "2.0.0",
		Meta: &upstreamv0.ServerMeta{
			PublisherProvided: map[string]any{
				"provider": map[string]any{
					"toolhive": map[string]any{
						"metadata": map[string]any{
							"stars": float64(200),
							"pulls": float64(1000),
						},
						"tier":      "Official",
						"status":    "Active",
						"transport": "http",
					},
				},
			},
		},
	}

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v0/servers/io.test/server1":
			json.NewEncoder(w).Encode(server1)
		case "/v0/servers/io.test/server2":
			json.NewEncoder(w).Encode(server2)
		default:
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	// Create MCP server with test API URL
	mcpServer := NewServer(testServer.URL)

	params := &CompareServersParams{
		ServerNames: []string{"io.test/server1", "io.test/server2"},
	}

	result, _, err := mcpServer.compareServers(context.Background(), nil, params)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)

	textContent := result.Content[0].(*sdkmcp.TextContent)
	assert.Contains(t, textContent.Text, "Server Comparison")
	assert.Contains(t, textContent.Text, "io.test/server1")
	assert.Contains(t, textContent.Text, "io.test/server2")
	assert.Contains(t, textContent.Text, "100") // stars for server1
	assert.Contains(t, textContent.Text, "200") // stars for server2
}

func TestHandleSearchServers_WithTags(t *testing.T) {
	t.Parallel()

	servers := []upstreamv0.ServerJSON{
		{
			Name:        "io.test/database-server",
			Description: "A database server",
			Version:     "1.0.0",
			Meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]any{
					"provider": map[string]any{
						"package": map[string]any{
							"tags": []any{"database", "sql"},
							"metadata": map[string]any{
								"stars": float64(100),
							},
						},
					},
				},
			},
		},
		{
			Name:        "io.test/file-server",
			Description: "A file server",
			Version:     "1.0.0",
			Meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]any{
					"provider": map[string]any{
						"package": map[string]any{
							"tags": []any{"files", "storage"},
							"metadata": map[string]any{
								"stars": float64(50),
							},
						},
					},
				},
			},
		},
	}

	// Create test HTTP server - returns ALL servers (client-side filtering)
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/servers" {
			w.Header().Set("Content-Type", "application/json")
			// Return official ServerListResponse format with all servers
			response := upstreamv0.ServerListResponse{
				Servers: []upstreamv0.ServerResponse{
					{Server: servers[0]},
					{Server: servers[1]},
				},
				Metadata: upstreamv0.Metadata{
					Count: 2,
				},
			}
			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer testServer.Close()

	// Create MCP server with test API URL
	mcpServer := NewServer(testServer.URL)

	params := &SearchServersParams{
		Query: "server",
		Tags:  []string{"database"},
		Limit: 5,
	}

	result, _, err := mcpServer.searchServers(context.Background(), nil, params)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)
	textContent := result.Content[0].(*sdkmcp.TextContent)
	assert.Contains(t, textContent.Text, "io.test/database-server")
	assert.NotContains(t, textContent.Text, "io.test/file-server")
}

func TestSearchServers_WithCursorIteration(t *testing.T) {
	t.Parallel()

	// Create test servers for 2 pages
	page1Servers := []upstreamv0.ServerJSON{
		{
			Name:        "io.test/server1",
			Description: "Server 1",
			Version:     "1.0.0",
		},
		{
			Name:        "io.test/server2",
			Description: "Server 2",
			Version:     "2.0.0",
		},
	}

	page2Servers := []upstreamv0.ServerJSON{
		{
			Name:        "io.test/server3",
			Description: "Server 3",
			Version:     "3.0.0",
		},
	}

	// Track API calls
	callCount := 0

	// Create test HTTP server that simulates pagination
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/servers" {
			w.Header().Set("Content-Type", "application/json")

			cursor := r.URL.Query().Get("cursor")
			var response upstreamv0.ServerListResponse

			if cursor == "" {
				// First page: return servers 1-2 with nextCursor
				response = upstreamv0.ServerListResponse{
					Servers: []upstreamv0.ServerResponse{
						{Server: page1Servers[0]},
						{Server: page1Servers[1]},
					},
					Metadata: upstreamv0.Metadata{
						Count:      2,
						NextCursor: "page2cursor",
					},
				}
				callCount++
			} else if cursor == "page2cursor" {
				// Second page: return server 3 with no nextCursor
				response = upstreamv0.ServerListResponse{
					Servers: []upstreamv0.ServerResponse{
						{Server: page2Servers[0]},
					},
					Metadata: upstreamv0.Metadata{
						Count:      1,
						NextCursor: "", // No more pages
					},
				}
				callCount++
			}

			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer testServer.Close()

	// Create MCP server with test API URL
	mcpServer := NewServer(testServer.URL)

	// First call: no cursor, should return page 1 with nextCursor
	// Set limit to 2 to match first page size, so it stops after first page
	params1 := &SearchServersParams{
		Limit: 2,
	}

	result1, _, err := mcpServer.searchServers(context.Background(), nil, params1)
	require.NoError(t, err)
	assert.False(t, result1.IsError)

	// Parse first response
	textContent1 := result1.Content[0].(*sdkmcp.TextContent)
	var response1 struct {
		Servers  []upstreamv0.ServerResponse `json:"servers"`
		Metadata struct {
			Count      int    `json:"count"`
			NextCursor string `json:"nextCursor"`
		} `json:"metadata"`
	}
	err = json.Unmarshal([]byte(textContent1.Text), &response1)
	require.NoError(t, err)

	// Verify first response
	assert.Equal(t, 2, len(response1.Servers), "First call should return 2 servers")
	assert.Equal(t, "page2cursor", response1.Metadata.NextCursor, "Should have nextCursor")
	assert.Equal(t, "io.test/server1", response1.Servers[0].Server.Name)
	assert.Equal(t, "io.test/server2", response1.Servers[1].Server.Name)

	// Second call: with cursor, should return page 2 without nextCursor
	params2 := &SearchServersParams{
		Cursor: "page2cursor",
		Limit:  2,
	}

	result2, _, err := mcpServer.searchServers(context.Background(), nil, params2)
	require.NoError(t, err)
	assert.False(t, result2.IsError)

	// Parse second response
	textContent2 := result2.Content[0].(*sdkmcp.TextContent)
	var response2 struct {
		Servers  []upstreamv0.ServerResponse `json:"servers"`
		Metadata struct {
			Count      int    `json:"count"`
			NextCursor string `json:"nextCursor"`
		} `json:"metadata"`
	}
	err = json.Unmarshal([]byte(textContent2.Text), &response2)
	require.NoError(t, err)

	// Verify second response
	assert.Equal(t, 1, len(response2.Servers), "Second call should return 1 server")
	assert.Empty(t, response2.Metadata.NextCursor, "Should have no nextCursor (end of results)")
	assert.Equal(t, "io.test/server3", response2.Servers[0].Server.Name)

	// Verify pagination happened (2 API calls)
	assert.Equal(t, 2, callCount, "Should have made 2 API calls total")
}

// TestHandleCompareServers_InvalidArgs removed - SDK validates parameters automatically via jsonschema
