// Package mcp provides MCP (Model Context Protocol) server implementation
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/stacklok/toolhive/pkg/logger"
)

// Transport handles HTTP/SSE communication for MCP
type Transport struct {
	server *Server
}

// NewTransport creates a new transport layer
func NewTransport(server *Server) *Transport {
	return &Transport{
		server: server,
	}
}

// ServeHTTP implements http.Handler for standard HTTP POST requests
func (t *Transport) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Errorf("Failed to read request body: %v", err)
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Validate and parse request
	req, err := ValidateRequest(body)
	if err != nil {
		logger.Errorf("Invalid request: %v", err)
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &RPCError{
				Code:    ParseError,
				Message: err.Error(),
			},
		}
		t.writeJSONResponse(w, resp)
		return
	}

	// Process request
	resp := t.server.HandleRequest(r.Context(), *req)

	// Write response
	t.writeJSONResponse(w, resp)
}

// ServeSSE handles Server-Sent Events connections for streaming
func (t *Transport) ServeSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create a context that will be cancelled when the client disconnects
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Check if the response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	logger.Info("SSE client connected")

	// Send initial connection message
	t.sendSSEMessage(w, flusher, map[string]any{
		"type":    "connection",
		"message": "Connected to ToolHive Registry MCP Server",
	})

	// Handle incoming messages from client
	// In a real SSE implementation, we'd need to handle bidirectional communication
	// For now, we'll use a simple approach where the client sends requests via POST
	// and receives responses via SSE

	// Keep connection alive
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("SSE client disconnected")
			return
		case <-ticker.C:
			// Send ping to keep connection alive
			t.sendSSEMessage(w, flusher, map[string]any{
				"type": "ping",
			})
		}
	}
}

// ServeJSONRPC handles JSON-RPC requests over HTTP with streaming support
func (t *Transport) ServeJSONRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if client wants streaming responses
	acceptHeader := r.Header.Get("Accept")
	streaming := acceptHeader == "text/event-stream"

	if streaming {
		t.handleStreamingRequest(w, r)
	} else {
		t.ServeHTTP(w, r)
	}
}

// handleStreamingRequest handles requests with streaming responses
func (t *Transport) handleStreamingRequest(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Errorf("Failed to read request body: %v", err)
		t.sendSSEError(w, flusher, ParseError, "Failed to read request")
		return
	}
	defer r.Body.Close()

	// Validate and parse request
	req, err := ValidateRequest(body)
	if err != nil {
		logger.Errorf("Invalid request: %v", err)
		t.sendSSEError(w, flusher, ParseError, err.Error())
		return
	}

	// Process request
	resp := t.server.HandleRequest(r.Context(), *req)

	// Send response as SSE
	t.sendSSEMessage(w, flusher, resp)
}

// ServeStdio handles stdio-based communication (for standalone mode)
func (t *Transport) ServeStdio(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	scanner := bufio.NewScanner(stdin)
	encoder := json.NewEncoder(stdout)

	logger.Info("MCP server started in stdio mode")

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Validate and parse request
		req, err := ValidateRequest(line)
		if err != nil {
			logger.Errorf("Invalid request: %v", err)
			resp := JSONRPCResponse{
				JSONRPC: "2.0",
				Error: &RPCError{
					Code:    ParseError,
					Message: err.Error(),
				},
			}
			if encErr := encoder.Encode(resp); encErr != nil {
				logger.Errorf("Failed to encode error response: %v", encErr)
			}
			continue
		}

		// Process request
		resp := t.server.HandleRequest(ctx, *req)

		// Write response
		if err := encoder.Encode(resp); err != nil {
			logger.Errorf("Failed to encode response: %v", err)
			return fmt.Errorf("failed to encode response: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	return nil
}

// writeJSONResponse writes a JSON response
func (*Transport) writeJSONResponse(w http.ResponseWriter, resp JSONRPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Errorf("Failed to encode response: %v", err)
	}
}

// sendSSEMessage sends a message via Server-Sent Events
func (*Transport) sendSSEMessage(w http.ResponseWriter, flusher http.Flusher, data any) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		logger.Errorf("Failed to marshal SSE data: %v", err)
		return
	}

	_, err = fmt.Fprintf(w, "data: %s\n\n", jsonData)
	if err != nil {
		logger.Errorf("Failed to write SSE message: %v", err)
		return
	}

	flusher.Flush()
}

// sendSSEError sends an error via Server-Sent Events
func (t *Transport) sendSSEError(w http.ResponseWriter, flusher http.Flusher, code int, message string) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
	t.sendSSEMessage(w, flusher, resp)
}

