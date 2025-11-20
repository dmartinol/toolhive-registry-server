// Package mcp provides MCP (Model Context Protocol) server implementation
package mcp

import (
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stacklok/toolhive-registry-server/internal/service"
)

const (
	// MCPVersion is the version of the MCP server implementation
	MCPVersion = "1.0.0"
)

// Server represents an MCP server instance wrapping the SDK server
type Server struct {
	service   service.RegistryService
	sdkServer *sdkmcp.Server
}

// NewServer creates a new MCP server using the official Go SDK
func NewServer(svc service.RegistryService) *Server {
	s := &Server{
		service: svc,
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
	// Register search_servers tool
	sdkmcp.AddTool(s.sdkServer, &sdkmcp.Tool{
		Name:        "search_servers",
		Description: "Search for MCP servers by keywords, tags, or use case. Supports natural language queries.",
	}, s.searchServers)

	// Register get_server_details tool
	sdkmcp.AddTool(s.sdkServer, &sdkmcp.Tool{
		Name:        "get_server_details",
		Description: "Get comprehensive information about a specific MCP server including packages, metadata, and ToolHive-specific data.",
	}, s.getServerDetails)

	// Register list_servers tool
	sdkmcp.AddTool(s.sdkServer, &sdkmcp.Tool{
		Name:        "list_servers",
		Description: "List all available MCP servers with filtering and pagination support.",
	}, s.listServers)

	// Register compare_servers tool
	sdkmcp.AddTool(s.sdkServer, &sdkmcp.Tool{
		Name:        "compare_servers",
		Description: "Compare multiple MCP servers side-by-side showing features, statistics, and differences.",
	}, s.compareServers)
}
