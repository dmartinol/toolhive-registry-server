// Package mcp provides MCP (Model Context Protocol) server implementation
package mcp

import (
	"encoding/json"
)

// Protocol version
const (
	ProtocolVersion = "2024-11-05"
	MCPVersion      = "1.0.0"
)

// JSON-RPC 2.0 request/response types

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      any         `json:"id,omitempty"`
	Result  any         `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// MCP Protocol types

// InitializeParams contains parameters for initialization
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo     `json:"clientInfo"`
}

// ClientCapabilities describes client capabilities
type ClientCapabilities struct {
	Roots    *RootsCapability    `json:"roots,omitempty"`
	Sampling *SamplingCapability `json:"sampling,omitempty"`
}

// RootsCapability describes client root capabilities
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// SamplingCapability describes client sampling capabilities
type SamplingCapability struct{}

// ClientInfo contains information about the client
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the result of initialization
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
}

// ServerCapabilities describes server capabilities
type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
}

// ToolsCapability describes server tool capabilities
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability describes server resource capabilities
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability describes server prompt capabilities
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ServerInfo contains information about the server
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Tool represents an MCP tool
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema describes the JSON schema for tool input
type InputSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]PropertyDef `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

// PropertyDef describes a property in the input schema
type PropertyDef struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Items       *ItemDef `json:"items,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// ItemDef describes array items in the input schema
type ItemDef struct {
	Type string `json:"type"`
}

// ListToolsResult is the result of listing tools
type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

// CallToolParams contains parameters for calling a tool
type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// CallToolResult is the result of calling a tool
type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Content represents tool result content
type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ListResourcesResult is the result of listing resources
type ListResourcesResult struct {
	Resources []Resource `json:"resources"`
}

// Resource represents an MCP resource
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ReadResourceParams contains parameters for reading a resource
type ReadResourceParams struct {
	URI string `json:"uri"`
}

// ReadResourceResult is the result of reading a resource
type ReadResourceResult struct {
	Contents []ResourceContent `json:"contents"`
}

// ResourceContent represents resource content
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

