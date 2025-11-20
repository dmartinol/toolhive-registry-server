// Package mcp provides MCP (Model Context Protocol) server implementation
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/service"
)

// Server represents an MCP server instance
type Server struct {
	service service.RegistryService
	tools   map[string]ToolHandler
}

// ToolHandler is a function that handles tool execution
type ToolHandler func(ctx context.Context, args map[string]any) (CallToolResult, error)

// NewServer creates a new MCP server
func NewServer(svc service.RegistryService) *Server {
	s := &Server{
		service: svc,
		tools:   make(map[string]ToolHandler),
	}

	// Register tool handlers
	s.registerTools()

	return s
}

// registerTools registers all available tool handlers
func (s *Server) registerTools() {
	s.tools["search_servers"] = s.handleSearchServers
	s.tools["get_server_details"] = s.handleGetServerDetails
	s.tools["list_servers"] = s.handleListServers
	s.tools["compare_servers"] = s.handleCompareServers
}

// HandleRequest processes an MCP JSON-RPC request
func (s *Server) HandleRequest(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	// Validate JSON-RPC version
	if req.JSONRPC != "2.0" {
		return s.errorResponse(req.ID, InvalidRequest, "Invalid JSON-RPC version")
	}

	// Route to appropriate handler based on method
	switch req.Method {
	case "initialize":
		return s.handleInitialize(ctx, req)
	case "tools/list":
		return s.handleListTools(ctx, req)
	case "tools/call":
		return s.handleCallTool(ctx, req)
	case "resources/list":
		return s.handleListResources(ctx, req)
	case "resources/read":
		return s.handleReadResource(ctx, req)
	default:
		return s.errorResponse(req.ID, MethodNotFound, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

// handleInitialize handles the initialize request
func (s *Server) handleInitialize(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	var params InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.errorResponse(req.ID, InvalidParams, "Invalid initialize parameters")
		}
	}

	logger.Infof("MCP client connected: %s v%s", params.ClientInfo.Name, params.ClientInfo.Version)

	result := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools: &ToolsCapability{
				ListChanged: false,
			},
			Resources: &ResourcesCapability{
				Subscribe:   false,
				ListChanged: false,
			},
		},
		ServerInfo: ServerInfo{
			Name:    "toolhive-registry-mcp",
			Version: MCPVersion,
		},
	}

	return s.successResponse(req.ID, result)
}

// handleListTools handles the tools/list request
func (s *Server) handleListTools(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	tools := []Tool{
		{
			Name:        "search_servers",
			Description: "Search for MCP servers by keywords, tags, or use case. Supports natural language queries.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"query": {
						Type:        "string",
						Description: "Natural language query or keywords to search for (e.g., 'database tool', 'file operations')",
					},
					"tags": {
						Type:        "array",
						Description: "Optional array of tags to filter by",
						Items: &ItemDef{
							Type: "string",
						},
					},
					"limit": {
						Type:        "number",
						Description: "Maximum number of results to return (default: 10)",
					},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "get_server_details",
			Description: "Get comprehensive information about a specific MCP server including packages, metadata, and ToolHive-specific data.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"server_name": {
						Type:        "string",
						Description: "Fully qualified server name (e.g., 'io.github.stacklok/everything')",
					},
					"version": {
						Type:        "string",
						Description: "Specific version or 'latest' (default: 'latest')",
					},
				},
				Required: []string{"server_name"},
			},
		},
		{
			Name:        "list_servers",
			Description: "List all available MCP servers with filtering and pagination support.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"cursor": {
						Type:        "string",
						Description: "Pagination cursor for retrieving next set of results",
					},
					"limit": {
						Type:        "number",
						Description: "Maximum number of results per page (default: 20)",
					},
					"version_filter": {
						Type:        "string",
						Description: "Filter by version: 'latest' or specific version",
					},
					"sort_by": {
						Type:        "string",
						Description: "Sort results by field",
						Enum:        []string{"stars", "pulls", "updated_at", "name"},
					},
				},
			},
		},
		{
			Name:        "compare_servers",
			Description: "Compare multiple MCP servers side-by-side showing features, statistics, and differences.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"server_names": {
						Type:        "array",
						Description: "List of server names to compare (2-5 servers)",
						Items: &ItemDef{
							Type: "string",
						},
					},
				},
				Required: []string{"server_names"},
			},
		},
	}

	result := ListToolsResult{
		Tools: tools,
	}

	return s.successResponse(req.ID, result)
}

// handleCallTool handles the tools/call request
func (s *Server) handleCallTool(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.errorResponse(req.ID, InvalidParams, "Invalid tool call parameters")
	}

	// Get the tool handler
	handler, ok := s.tools[params.Name]
	if !ok {
		return s.errorResponse(req.ID, MethodNotFound, fmt.Sprintf("Tool not found: %s", params.Name))
	}

	// Execute the tool
	result, err := handler(ctx, params.Arguments)
	if err != nil {
		logger.Errorf("Tool execution error for %s: %v", params.Name, err)
		return s.errorResponse(req.ID, InternalError, fmt.Sprintf("Tool execution failed: %v", err))
	}

	return s.successResponse(req.ID, result)
}

// handleListResources handles the resources/list request
func (s *Server) handleListResources(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	// For now, we don't expose resources, only tools
	result := ListResourcesResult{
		Resources: []Resource{},
	}

	return s.successResponse(req.ID, result)
}

// handleReadResource handles the resources/read request
func (s *Server) handleReadResource(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	return s.errorResponse(req.ID, MethodNotFound, "Resources are not supported")
}

// successResponse creates a success JSON-RPC response
func (*Server) successResponse(id any, result any) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

// errorResponse creates an error JSON-RPC response
func (*Server) errorResponse(id any, code int, message string) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
}

// ValidateRequest validates a JSON-RPC request
func ValidateRequest(data []byte) (*JSONRPCRequest, error) {
	var req JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errors.New("invalid JSON")
	}

	if req.JSONRPC != "2.0" {
		return nil, errors.New("invalid JSON-RPC version")
	}

	if req.Method == "" {
		return nil, errors.New("method is required")
	}

	return &req, nil
}

