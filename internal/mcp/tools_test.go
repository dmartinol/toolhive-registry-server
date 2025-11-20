package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
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

func TestHandleListServers(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRegistryService(ctrl)
	server := NewServer(mockSvc)

	servers := []upstreamv0.ServerJSON{
		{
			Name:        "io.test/server1",
			Description: "Server 1",
			Version:     "1.0.0",
			Meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]any{
					"provider": map[string]any{
						"toolhive": map[string]any{
							"metadata": map[string]any{
								"stars": float64(100),
								"pulls": float64(500),
							},
						},
					},
				},
			},
		},
		{
			Name:        "io.test/server2",
			Description: "Server 2",
			Version:     "1.0.0",
			Meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]any{
					"provider": map[string]any{
						"toolhive": map[string]any{
							"metadata": map[string]any{
								"stars": float64(200),
								"pulls": float64(1000),
							},
						},
					},
				},
			},
		},
	}

	mockSvc.EXPECT().ListServers(context.Background()).Return(servers, nil)

	args := map[string]any{
		"limit":   float64(10),
		"sort_by": "stars",
	}

	result, err := server.handleListServers(context.Background(), args)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)
	assert.Equal(t, "text", result.Content[0].Type)
	assert.Contains(t, result.Content[0].Text, "io.test/server1")
	assert.Contains(t, result.Content[0].Text, "io.test/server2")
}

func TestHandleCompareServers(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRegistryService(ctrl)
	server := NewServer(mockSvc)

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

	mockSvc.EXPECT().GetServer(context.Background(), "io.test/server1").Return(server1, nil)
	mockSvc.EXPECT().GetServer(context.Background(), "io.test/server2").Return(server2, nil)

	args := map[string]any{
		"server_names": []any{"io.test/server1", "io.test/server2"},
	}

	result, err := server.handleCompareServers(context.Background(), args)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)
	assert.Equal(t, "text", result.Content[0].Type)
	
	text := result.Content[0].Text
	assert.Contains(t, text, "Server Comparison")
	assert.Contains(t, text, "io.test/server1")
	assert.Contains(t, text, "io.test/server2")
	assert.Contains(t, text, "100")  // stars for server1
	assert.Contains(t, text, "200")  // stars for server2
}

func TestHandleSearchServers_WithTags(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRegistryService(ctrl)
	server := NewServer(mockSvc)

	servers := []upstreamv0.ServerJSON{
		{
			Name:        "io.test/database-server",
			Description: "A database server",
			Version:     "1.0.0",
			Meta: &upstreamv0.ServerMeta{
				PublisherProvided: map[string]any{
					"provider": map[string]any{
						"metadata": map[string]any{
							"tags": []any{"database", "sql"},
						},
						"toolhive": map[string]any{
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
						"metadata": map[string]any{
							"tags": []any{"files", "storage"},
						},
						"toolhive": map[string]any{
							"metadata": map[string]any{
								"stars": float64(50),
							},
						},
					},
				},
			},
		},
	}

	mockSvc.EXPECT().ListServers(context.Background()).Return(servers, nil)

	args := map[string]any{
		"query": "server",
		"tags":  []any{"database"},
		"limit": float64(5),
	}

	result, err := server.handleSearchServers(context.Background(), args)
	require.NoError(t, err)

	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)
	assert.Contains(t, result.Content[0].Text, "io.test/database-server")
	assert.NotContains(t, result.Content[0].Text, "io.test/file-server")
}

func TestHandleCompareServers_InvalidArgs(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSvc := mocks.NewMockRegistryService(ctrl)
	server := NewServer(mockSvc)

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "missing server_names",
			args: map[string]any{},
		},
		{
			name: "too few servers",
			args: map[string]any{
				"server_names": []any{"io.test/server1"},
			},
		},
		{
			name: "too many servers",
			args: map[string]any{
				"server_names": []any{
					"io.test/server1",
					"io.test/server2",
					"io.test/server3",
					"io.test/server4",
					"io.test/server5",
					"io.test/server6",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := server.handleCompareServers(context.Background(), tt.args)
			require.NoError(t, err)

			assert.True(t, result.IsError)
		})
	}
}

