// Package mcp provides MCP (Model Context Protocol) server implementation
package mcp

import (
	"context"
	"net/http"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

const (
	// MCPVersion is the version of the MCP server implementation
	MCPVersion = "2.0.0"
)

// RegistryAPIClient wraps HTTP client for Registry API calls
type RegistryAPIClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewRegistryAPIClient creates a new Registry API HTTP client
func NewRegistryAPIClient(baseURL string) *RegistryAPIClient {
	return &RegistryAPIClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Server represents an MCP server instance wrapping the SDK server
type Server struct {
	apiClient  *RegistryAPIClient
	localCache ServerCache // For integrated mode only
	sdkServer  *sdkmcp.Server
}

// ServerCache interface for local data access (used in integrated mode)
type ServerCache interface {
	ListServers(ctx context.Context) ([]upstreamv0.ServerJSON, error)
	GetServer(ctx context.Context, name string) (upstreamv0.ServerJSON, error)
}

// NewServer creates a new MCP server using the official Go SDK
// It connects to an existing Registry API server at the given URL
func NewServer(registryURL string) *Server {
	s := &Server{
		apiClient: NewRegistryAPIClient(registryURL),
	}

	// Create SDK server
	s.sdkServer = sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "toolhive-registry-mcp",
		Version: MCPVersion,
	}, nil)

	// Register all tools with automatic schema generation
	s.registerTools()

	return s
}

// NewServerWithCache creates an MCP server for integrated mode using local service
// This is used when the MCP server is embedded in the Registry API server
func NewServerWithCache(cache ServerCache) *Server {
	s := &Server{
		localCache: cache,
	}

	// Create SDK server
	s.sdkServer = sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "toolhive-registry-mcp",
		Version: MCPVersion,
	}, nil)

	// Register all tools with automatic schema generation
	s.registerTools()

	return s
}

// GetSDKServer returns the underlying SDK server for transport use
func (s *Server) GetSDKServer() *sdkmcp.Server {
	return s.sdkServer
}

// registerTools registers all available tool handlers with the SDK
func (s *Server) registerTools() {
	// Register search_servers tool - unified search/list/filter with cursor-based pagination
	sdkmcp.AddTool(s.sdkServer, &sdkmcp.Tool{
		Name: "search_servers",
		Description: "Search and filter MCP servers with comprehensive criteria. " +
			"Returns a single page (default 20 results, max 1000) with cursor-based pagination for complete results. " +
			"Supports filtering by name, tags, tools, transport, stars, pulls, tier, and status.",
	}, s.searchServers)

	// Register get_server_details tool
	sdkmcp.AddTool(s.sdkServer, &sdkmcp.Tool{
		Name: "get_server_details",
		Description: "Get comprehensive information about a specific MCP server including " +
			"packages, metadata, and ToolHive-specific data.",
	}, s.getServerDetails)

	// Register compare_servers tool
	sdkmcp.AddTool(s.sdkServer, &sdkmcp.Tool{
		Name:        "compare_servers",
		Description: "Compare multiple MCP servers side-by-side showing features, statistics, and differences.",
	}, s.compareServers)
}
