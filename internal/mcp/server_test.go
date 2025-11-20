package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

func TestServer_HandleInitialize(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRegistryService(ctrl)
	server := NewServer(mockSvc)

	params := InitializeParams{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    ClientCapabilities{},
		ClientInfo: ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  paramsJSON,
	}

	resp := server.HandleRequest(context.Background(), req)

	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 1, resp.ID)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)

	// Check result structure
	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result InitializeResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.Equal(t, ProtocolVersion, result.ProtocolVersion)
	assert.Equal(t, "toolhive-registry-mcp", result.ServerInfo.Name)
	assert.NotNil(t, result.Capabilities.Tools)
}

func TestServer_HandleListTools(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRegistryService(ctrl)
	server := NewServer(mockSvc)

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}

	resp := server.HandleRequest(context.Background(), req)

	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 1, resp.ID)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)

	// Check result structure
	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result ListToolsResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	// Verify we have all expected tools
	assert.Len(t, result.Tools, 4)

	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}

	assert.True(t, toolNames["search_servers"])
	assert.True(t, toolNames["get_server_details"])
	assert.True(t, toolNames["list_servers"])
	assert.True(t, toolNames["compare_servers"])
}

func TestServer_HandleCallTool_SearchServers(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRegistryService(ctrl)
	server := NewServer(mockSvc)

	// Mock server data
	servers := []upstreamv0.ServerJSON{
		{
			Name:        "io.test/database-server",
			Description: "A database server for testing",
			Version:     "1.0.0",
			Meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]any{
					"provider": map[string]any{
						"toolhive": map[string]any{
							"metadata": map[string]any{
								"stars": float64(100),
								"pulls": float64(500),
							},
							"tools": []any{"query", "connect"},
						},
					},
				},
			},
		},
	}

	mockSvc.EXPECT().ListServers(context.Background()).Return(servers, nil)

	params := CallToolParams{
		Name: "search_servers",
		Arguments: map[string]any{
			"query": "database",
			"limit": float64(5),
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  paramsJSON,
	}

	resp := server.HandleRequest(context.Background(), req)

	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 1, resp.ID)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)

	// Check result structure
	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result CallToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)
	assert.Equal(t, "text", result.Content[0].Type)
	assert.Contains(t, result.Content[0].Text, "io.test/database-server")
}

func TestServer_HandleCallTool_GetServerDetails(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRegistryService(ctrl)
	server := NewServer(mockSvc)

	// Mock server data
	serverData := upstreamv0.ServerJSON{
		Name:        "io.test/test-server",
		Description: "A test server",
		Version:     "1.0.0",
		Meta: &upstreamv0.ServerMeta{
			PublisherProvided: map[string]any{
				"provider": map[string]any{
					"toolhive": map[string]any{
						"metadata": map[string]any{
							"stars": float64(200),
							"pulls": float64(1000),
						},
						"tier":   "Community",
						"status": "Active",
					},
				},
			},
		},
	}

	mockSvc.EXPECT().GetServer(context.Background(), "io.test/test-server").Return(serverData, nil)

	params := CallToolParams{
		Name: "get_server_details",
		Arguments: map[string]any{
			"server_name": "io.test/test-server",
		},
	}

	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  paramsJSON,
	}

	resp := server.HandleRequest(context.Background(), req)

	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 1, resp.ID)
	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)

	// Check result structure
	resultJSON, err := json.Marshal(resp.Result)
	require.NoError(t, err)

	var result CallToolResult
	err = json.Unmarshal(resultJSON, &result)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)
	assert.Equal(t, "text", result.Content[0].Type)
	assert.Contains(t, result.Content[0].Text, "io.test/test-server")
	assert.Contains(t, result.Content[0].Text, "Stars: 200")
}

func TestServer_HandleInvalidMethod(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRegistryService(ctrl)
	server := NewServer(mockSvc)

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "invalid/method",
	}

	resp := server.HandleRequest(context.Background(), req)

	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 1, resp.ID)
	assert.NotNil(t, resp.Error)
	assert.Equal(t, MethodNotFound, resp.Error.Code)
}

func TestValidateRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{
			name: "valid request",
			data: `{"jsonrpc":"2.0","method":"test","id":1}`,
			wantErr: false,
		},
		{
			name: "invalid JSON",
			data: `{invalid}`,
			wantErr: true,
		},
		{
			name: "missing method",
			data: `{"jsonrpc":"2.0","id":1}`,
			wantErr: true,
		},
		{
			name: "invalid JSON-RPC version",
			data: `{"jsonrpc":"1.0","method":"test","id":1}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := ValidateRequest([]byte(tt.data))

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, req)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, req)
			}
		})
	}
}

