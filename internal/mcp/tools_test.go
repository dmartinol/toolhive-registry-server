package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testServersPath = "/v0/servers"

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

	// Create test HTTP server using the official MCP Registry API format
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v0/servers/io.test/server1/versions/latest":
			json.NewEncoder(w).Encode(map[string]any{"server": server1})
		case "/v0/servers/io.test/server2/versions/latest":
			json.NewEncoder(w).Encode(map[string]any{"server": server2})
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
		if r.URL.Path == testServersPath {
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
		if r.URL.Path == testServersPath {
			w.Header().Set("Content-Type", "application/json")

			cursor := r.URL.Query().Get("cursor")
			var response upstreamv0.ServerListResponse

			switch cursor {
			case "":
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
			case "page2cursor":
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

func TestMatchesRegistryTypeFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		server       upstreamv0.ServerJSON
		registryType string
		expected     bool
	}{
		{
			name: "matches oci registry type",
			server: upstreamv0.ServerJSON{
				Packages: []model.Package{
					{
						RegistryType: "oci",
						Identifier:   "ghcr.io/example/server:latest",
					},
				},
			},
			registryType: "oci",
			expected:     true,
		},
		{
			name: "matches npm registry type case-insensitive",
			server: upstreamv0.ServerJSON{
				Packages: []model.Package{
					{
						RegistryType: "npm",
						Identifier:   "@modelcontextprotocol/server-example",
					},
				},
			},
			registryType: "NPM",
			expected:     true,
		},
		{
			name: "does not match different registry type",
			server: upstreamv0.ServerJSON{
				Packages: []model.Package{
					{
						RegistryType: "npm",
						Identifier:   "@modelcontextprotocol/server-example",
					},
				},
			},
			registryType: "oci",
			expected:     false,
		},
		{
			name: "matches when one of multiple packages has registry type",
			server: upstreamv0.ServerJSON{
				Packages: []model.Package{
					{
						RegistryType: "npm",
						Identifier:   "@modelcontextprotocol/server-example",
					},
					{
						RegistryType: "oci",
						Identifier:   "ghcr.io/example/server:latest",
					},
				},
			},
			registryType: "oci",
			expected:     true,
		},
		{
			name: "empty filter matches all",
			server: upstreamv0.ServerJSON{
				Packages: []model.Package{
					{
						RegistryType: "npm",
						Identifier:   "@modelcontextprotocol/server-example",
					},
				},
			},
			registryType: "",
			expected:     true,
		},
		{
			name: "no packages does not match",
			server: upstreamv0.ServerJSON{
				Packages: []model.Package{},
			},
			registryType: "oci",
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := &Server{}
			result := s.matchesRegistryTypeFilter(tt.server, tt.registryType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSearchServers_WithRegistryTypeFilter(t *testing.T) {
	t.Parallel()

	// Create test servers with different registry types
	servers := []upstreamv0.ServerJSON{
		{
			Name:        "io.test/oci-server",
			Description: "An OCI-based server",
			Version:     "1.0.0",
			Packages: []model.Package{
				{
					RegistryType: "oci",
					Identifier:   "ghcr.io/test/oci-server:latest",
				},
			},
		},
		{
			Name:        "io.test/npm-server",
			Description: "An NPM-based server",
			Version:     "1.0.0",
			Packages: []model.Package{
				{
					RegistryType: "npm",
					Identifier:   "@test/npm-server",
				},
			},
		},
		{
			Name:        "io.test/pypi-server",
			Description: "A PyPI-based server",
			Version:     "1.0.0",
			Packages: []model.Package{
				{
					RegistryType: "pypi",
					Identifier:   "test-pypi-server",
				},
			},
		},
	}

	// Create test HTTP server - returns ALL servers (client-side filtering)
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == testServersPath {
			w.Header().Set("Content-Type", "application/json")
			// Return official ServerListResponse format with all servers
			response := upstreamv0.ServerListResponse{
				Servers: []upstreamv0.ServerResponse{
					{Server: servers[0]},
					{Server: servers[1]},
					{Server: servers[2]},
				},
				Metadata: upstreamv0.Metadata{
					Count: 3,
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
		RegistryType: "oci",
		Limit:        10,
	}

	result, _, err := mcpServer.searchServers(context.Background(), nil, params)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)
	textContent := result.Content[0].(*sdkmcp.TextContent)
	assert.Contains(t, textContent.Text, "io.test/oci-server")
	assert.NotContains(t, textContent.Text, "io.test/npm-server")
	assert.NotContains(t, textContent.Text, "io.test/pypi-server")
}

// TestHandleCompareServers_InvalidArgs removed - SDK validates parameters automatically via jsonschema

// Journey 1 tool tests

func TestGetSetupGuide_NPMPackage(t *testing.T) {
	t.Parallel()

	server := upstreamv0.ServerJSON{
		Name:        "io.test/postgres-server",
		Description: "PostgreSQL database server for MCP",
		Version:     "1.0.0",
		Repository: &model.Repository{
			URL:    "https://github.com/test/postgres-server",
			Source: "github",
		},
		Packages: []model.Package{
			{
				RegistryType: "npm",
				Identifier:   "@test/postgres-mcp",
				RunTimeHint:  "node",
				Transport: model.Transport{
					Type: "stdio",
				},
			},
		},
		Meta: &upstreamv0.ServerMeta{
			PublisherProvided: map[string]any{
				"provider": map[string]any{
					"package": map[string]any{
						"tags": []any{"database", "postgres", "sql"},
					},
				},
			},
		},
	}

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v0/servers/io.test/postgres-server/versions/latest" {
			json.NewEncoder(w).Encode(map[string]any{"server": server})
		} else {
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	// Create MCP server with test API URL
	mcpServer := NewServer(testServer.URL)

	params := &GetSetupGuideParams{
		ServerName: "io.test/postgres-server",
		Platform:   "claude-desktop",
	}

	result, _, err := mcpServer.getSetupGuide(context.Background(), nil, params)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)

	textContent := result.Content[0].(*sdkmcp.TextContent)
	guide := textContent.Text

	// Verify guide contains key sections
	assert.Contains(t, guide, "# Setup Guide: io.test/postgres-server")
	assert.Contains(t, guide, "PostgreSQL database server for MCP")
	assert.Contains(t, guide, "## Prerequisites")
	assert.Contains(t, guide, "**Runtime**: node")
	assert.Contains(t, guide, "**Transport**: stdio")
	assert.Contains(t, guide, "## Installation")
	assert.Contains(t, guide, "npm install -g @test/postgres-mcp")
	assert.Contains(t, guide, "npx @test/postgres-mcp")
	assert.Contains(t, guide, "## Environment Variables")
	assert.Contains(t, guide, "DATABASE_URL")
	assert.Contains(t, guide, "## Configuration")
	assert.Contains(t, guide, "Claude Desktop Configuration")
	assert.Contains(t, guide, "~/.config/claude/config.json")
	assert.Contains(t, guide, "Cursor Configuration")
	assert.Contains(t, guide, "## Troubleshooting")
	assert.Contains(t, guide, "## Next Steps")
	assert.Contains(t, guide, "https://github.com/test/postgres-server")
}

func TestGetSetupGuide_PythonPackage(t *testing.T) {
	t.Parallel()

	server := upstreamv0.ServerJSON{
		Name:        "io.test/python-server",
		Description: "Python-based MCP server",
		Version:     "2.0.0",
		Packages: []model.Package{
			{
				RegistryType: "pypi",
				Identifier:   "python-mcp-server",
				RunTimeHint:  "python",
				Transport: model.Transport{
					Type: "http",
				},
			},
		},
	}

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v0/servers/io.test/python-server/versions/latest" {
			json.NewEncoder(w).Encode(map[string]any{"server": server})
		} else {
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	mcpServer := NewServer(testServer.URL)

	params := &GetSetupGuideParams{
		ServerName: "io.test/python-server",
		Platform:   "cursor",
	}

	result, _, err := mcpServer.getSetupGuide(context.Background(), nil, params)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	textContent := result.Content[0].(*sdkmcp.TextContent)
	guide := textContent.Text

	// Verify Python-specific content
	assert.Contains(t, guide, "**Runtime**: python")
	assert.Contains(t, guide, "pip install python-mcp-server")
	assert.Contains(t, guide, "pipx install python-mcp-server")
	assert.Contains(t, guide, "python -m python-mcp-server")
}

func TestGetSetupGuide_DockerPackage(t *testing.T) {
	t.Parallel()

	server := upstreamv0.ServerJSON{
		Name:    "io.test/docker-server",
		Version: "3.0.0",
		Packages: []model.Package{
			{
				RegistryType: "docker",
				Identifier:   "testorg/mcp-server:latest",
				Transport: model.Transport{
					Type: "sse",
				},
			},
		},
	}

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v0/servers/io.test/docker-server/versions/latest" {
			json.NewEncoder(w).Encode(map[string]any{"server": server})
		} else {
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	mcpServer := NewServer(testServer.URL)

	params := &GetSetupGuideParams{
		ServerName: "io.test/docker-server",
	}

	result, _, err := mcpServer.getSetupGuide(context.Background(), nil, params)
	require.NoError(t, err)

	textContent := result.Content[0].(*sdkmcp.TextContent)
	guide := textContent.Text

	// Verify Docker-specific content
	assert.Contains(t, guide, "**Runtime**: docker")
	assert.Contains(t, guide, "docker pull testorg/mcp-server:latest")
	assert.Contains(t, guide, "docker run")
}

func TestFindAlternatives_HighSimilarity(t *testing.T) {
	t.Parallel()

	sourceServer := upstreamv0.ServerJSON{
		Name:        "io.test/postgres-mcp",
		Description: "PostgreSQL database connector for MCP",
		Version:     "1.0.0",
		Packages: []model.Package{
			{
				RegistryType: "npm",
				Transport: model.Transport{
					Type: "stdio",
				},
			},
		},
		Meta: &upstreamv0.ServerMeta{
			PublisherProvided: map[string]any{
				"provider": map[string]any{
					"package": map[string]any{
						"tags":  []any{"database", "sql", "postgres"},
						"tools": []any{"query", "execute", "transaction"},
						"metadata": map[string]any{
							"stars": float64(100),
						},
					},
				},
			},
		},
	}

	similarServer := upstreamv0.ServerJSON{
		Name:        "io.test/mysql-mcp",
		Description: "MySQL database connector for MCP",
		Version:     "2.0.0",
		Packages: []model.Package{
			{
				RegistryType: "npm",
				Transport: model.Transport{
					Type: "stdio",
				},
			},
		},
		Meta: &upstreamv0.ServerMeta{
			PublisherProvided: map[string]any{
				"provider": map[string]any{
					"package": map[string]any{
						"tags":  []any{"database", "sql", "mysql"},
						"tools": []any{"query", "execute", "transaction"},
						"metadata": map[string]any{
							"stars": float64(150),
						},
					},
				},
			},
		},
	}

	dissimilarServer := upstreamv0.ServerJSON{
		Name:        "io.test/file-server",
		Description: "File management server",
		Version:     "1.0.0",
		Packages: []model.Package{
			{
				RegistryType: "pypi",
				Transport: model.Transport{
					Type: "http",
				},
			},
		},
		Meta: &upstreamv0.ServerMeta{
			PublisherProvided: map[string]any{
				"provider": map[string]any{
					"package": map[string]any{
						"tags":  []any{"files", "storage"},
						"tools": []any{"read", "write"},
					},
				},
			},
		},
	}

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/v0/servers/io.test/postgres-mcp/versions/latest":
			json.NewEncoder(w).Encode(map[string]any{"server": sourceServer})
		case testServersPath:
			// Return list of all servers
			response := upstreamv0.ServerListResponse{
				Servers: []upstreamv0.ServerResponse{
					{Server: sourceServer},
					{Server: similarServer},
					{Server: dissimilarServer},
				},
				Metadata: upstreamv0.Metadata{
					Count: 3,
				},
			}
			json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	mcpServer := NewServer(testServer.URL)

	params := &FindAlternativesParams{
		ServerName: "io.test/postgres-mcp",
		Limit:      5,
	}

	result, _, err := mcpServer.findAlternatives(context.Background(), nil, params)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)

	textContent := result.Content[0].(*sdkmcp.TextContent)

	// Parse JSON response
	var response struct {
		Alternatives []struct {
			Server              upstreamv0.ServerResponse `json:"server"`
			SimilarityScore     float64                   `json:"similarityScore"`
			MatchReasons        []string                  `json:"matchReasons"`
			MigrationComplexity string                    `json:"migrationComplexity"`
		} `json:"alternatives"`
		Metadata struct {
			Count           int    `json:"count"`
			SourceServer    string `json:"sourceServer"`
			ScoringCriteria string `json:"scoringCriteria"`
		} `json:"metadata"`
	}

	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Should find MySQL as most similar, file server may or may not be included
	assert.Greater(t, len(response.Alternatives), 0, "Should find at least one alternative")
	assert.Equal(t, "io.test/mysql-mcp", response.Alternatives[0].Server.Server.Name)
	assert.Greater(t, response.Alternatives[0].SimilarityScore, 0.5, "MySQL should be highly similar")
	assert.Contains(t, response.Alternatives[0].MatchReasons[0], "shared tags")
	assert.Equal(t, "Low", response.Alternatives[0].MigrationComplexity, "Same tools = low complexity")

	// Verify metadata
	assert.Equal(t, "io.test/postgres-mcp", response.Metadata.SourceServer)
	assert.Contains(t, response.Metadata.ScoringCriteria, "tags(40%)")
}

func TestFindAlternatives_NoSimilarServers(t *testing.T) {
	t.Parallel()

	uniqueServer := upstreamv0.ServerJSON{
		Name:        "io.test/unique-server",
		Description: "Completely unique server",
		Version:     "1.0.0",
		Meta: &upstreamv0.ServerMeta{
			PublisherProvided: map[string]any{
				"provider": map[string]any{
					"package": map[string]any{
						"tags":  []any{"unique", "special"},
						"tools": []any{"unique_tool"},
					},
				},
			},
		},
	}

	differentServer := upstreamv0.ServerJSON{
		Name:        "io.test/different-server",
		Description: "Totally different server",
		Version:     "1.0.0",
		Meta: &upstreamv0.ServerMeta{
			PublisherProvided: map[string]any{
				"provider": map[string]any{
					"package": map[string]any{
						"tags":  []any{"different", "other"},
						"tools": []any{"different_tool"},
					},
				},
			},
		},
	}

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/v0/servers/io.test/unique-server/versions/latest":
			json.NewEncoder(w).Encode(map[string]any{"server": uniqueServer})
		case testServersPath:
			response := upstreamv0.ServerListResponse{
				Servers: []upstreamv0.ServerResponse{
					{Server: uniqueServer},
					{Server: differentServer},
				},
				Metadata: upstreamv0.Metadata{
					Count: 2,
				},
			}
			json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	mcpServer := NewServer(testServer.URL)

	params := &FindAlternativesParams{
		ServerName: "io.test/unique-server",
		Limit:      5,
	}

	result, _, err := mcpServer.findAlternatives(context.Background(), nil, params)
	require.NoError(t, err)

	assert.False(t, result.IsError)

	textContent := result.Content[0].(*sdkmcp.TextContent)
	var response struct {
		Alternatives []any `json:"alternatives"`
	}

	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	// Should find no or very few alternatives (similarity threshold > 0.1)
	assert.LessOrEqual(t, len(response.Alternatives), 1, "Should find very few alternatives")
}

func TestFindAlternatives_LimitParameter(t *testing.T) {
	t.Parallel()

	// Create source and 10 similar servers
	servers := make([]upstreamv0.ServerJSON, 11)
	servers[0] = upstreamv0.ServerJSON{
		Name:    "io.test/source",
		Version: "1.0.0",
		Meta: &upstreamv0.ServerMeta{
			PublisherProvided: map[string]any{
				"provider": map[string]any{
					"package": map[string]any{
						"tags": []any{"database", "sql"},
					},
				},
			},
		},
	}

	for i := 1; i < 11; i++ {
		servers[i] = upstreamv0.ServerJSON{
			Name:    fmt.Sprintf("io.test/similar%d", i),
			Version: "1.0.0",
			Meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]any{
					"provider": map[string]any{
						"package": map[string]any{
							"tags": []any{"database", "sql"},
						},
					},
				},
			},
		}
	}

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/v0/servers/io.test/source/versions/latest":
			json.NewEncoder(w).Encode(map[string]any{"server": servers[0]})
		case testServersPath:
			serverResponses := make([]upstreamv0.ServerResponse, len(servers))
			for i, srv := range servers {
				serverResponses[i] = upstreamv0.ServerResponse{Server: srv}
			}
			response := upstreamv0.ServerListResponse{
				Servers:  serverResponses,
				Metadata: upstreamv0.Metadata{Count: len(servers)},
			}
			json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	mcpServer := NewServer(testServer.URL)

	// Test with limit of 3
	params := &FindAlternativesParams{
		ServerName: "io.test/source",
		Limit:      3,
	}

	result, _, err := mcpServer.findAlternatives(context.Background(), nil, params)
	require.NoError(t, err)

	textContent := result.Content[0].(*sdkmcp.TextContent)
	var response struct {
		Alternatives []any `json:"alternatives"`
		Metadata     struct {
			Count int `json:"count"`
		} `json:"metadata"`
	}

	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err)

	assert.Equal(t, 3, len(response.Alternatives), "Should respect limit parameter")
	assert.Equal(t, 3, response.Metadata.Count)
}
