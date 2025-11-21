// Package mcp provides MCP (Model Context Protocol) server implementation
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stacklok/toolhive/pkg/logger"
)

const (
	// notAvailable is used when a field value is not present
	notAvailable = "N/A"
)

// Parameter structs for SDK tools with jsonschema tags for automatic schema generation

// SearchServersParams defines parameters for the search_servers tool with comprehensive filtering
type SearchServersParams struct {
	// Search & Filter
	Query     string   `json:"query,omitempty" jsonschema:"Natural language query or keywords"`
	Name      string   `json:"name,omitempty" jsonschema:"Filter by server name (substring match)"`
	Tags      []string `json:"tags,omitempty" jsonschema:"Filter by tags"`
	Tools     []string `json:"tools,omitempty" jsonschema:"Filter by tool names"`
	Transport string   `json:"transport,omitempty" jsonschema:"Filter by transport type (stdio, http, sse)"`

	// Metadata Filters
	MinStars int    `json:"min_stars,omitempty" jsonschema:"Minimum star count"`
	MinPulls int    `json:"min_pulls,omitempty" jsonschema:"Minimum pull count"`
	Tier     string `json:"tier,omitempty" jsonschema:"Filter by tier"`
	Status   string `json:"status,omitempty" jsonschema:"Filter by status"`

	// Pagination Control
	Cursor        string `json:"cursor,omitempty" jsonschema:"Pagination cursor from previous response (for iterating through results)"`
	Limit         int    `json:"limit,omitempty" jsonschema:"Max results per call (default: 20, max: 1000)"`
	VersionFilter string `json:"version_filter,omitempty" jsonschema:"Filter by version"`
	SortBy        string `json:"sort_by,omitempty" jsonschema:"Sort by: stars, pulls, name, updated_at"`
}

// GetServerDetailsParams defines parameters for the get_server_details tool
type GetServerDetailsParams struct {
	ServerName string `json:"server_name" jsonschema:"Fully qualified server name"`
	Version    string `json:"version,omitempty" jsonschema:"Specific version or 'latest' (default: 'latest')"`
}

// CompareServersParams defines parameters for the compare_servers tool
type CompareServersParams struct {
	ServerNames []string `json:"server_names" jsonschema:"List of server names to compare (2-5 servers)"`
}

// ToolHive metadata extraction helpers

// extractToolHiveMetadata extracts ToolHive-specific metadata from a server
func extractToolHiveMetadata(server upstreamv0.ServerJSON) map[string]any {
	metadata := make(map[string]any)

	if server.Meta == nil || server.Meta.PublisherProvided == nil {
		return metadata
	}

	// The PublisherProvided map already contains the provider namespace data
	// Structure: PublisherProvided[providerNamespace][packageIdentifier][metadata]
	// Example: PublisherProvided["io.github.stacklok"]["docker.io/mcp/everything:latest"]{stars, tools, etc}

	// Iterate through provider namespaces (e.g., "io.github.stacklok")
	for _, providerData := range server.Meta.PublisherProvided {
		providerMap, ok := providerData.(map[string]any)
		if !ok {
			continue
		}

		// Iterate through package identifiers (e.g., "docker.io/mcp/everything:latest")
		for _, packageData := range providerMap {
			packageMap, ok := packageData.(map[string]any)
			if !ok {
				continue
			}

			// Return the first package metadata found
			// (most servers have only one package)
			return packageMap
		}
	}

	return metadata
}

// extractStars extracts star count from ToolHive metadata
func extractStars(server upstreamv0.ServerJSON) int64 {
	thMeta := extractToolHiveMetadata(server)
	if meta, ok := thMeta["metadata"].(map[string]any); ok {
		if stars, ok := meta["stars"].(float64); ok {
			return int64(stars)
		}
	}
	return 0
}

// extractPulls extracts pull count from ToolHive metadata
func extractPulls(server upstreamv0.ServerJSON) int64 {
	thMeta := extractToolHiveMetadata(server)
	if meta, ok := thMeta["metadata"].(map[string]any); ok {
		if pulls, ok := meta["pulls"].(float64); ok {
			return int64(pulls)
		}
	}
	return 0
}

// extractTools extracts tool names from ToolHive metadata
func extractTools(server upstreamv0.ServerJSON) []string {
	thMeta := extractToolHiveMetadata(server)
	if tools, ok := thMeta["tools"].([]any); ok {
		result := make([]string, 0, len(tools))
		for _, tool := range tools {
			if toolStr, ok := tool.(string); ok {
				result = append(result, toolStr)
			}
		}
		return result
	}
	return []string{}
}

// Tool handler implementations (SDK signatures)

// searchServers implements the unified search_servers tool with chunked fetching
func (s *Server) searchServers(
	ctx context.Context, _ *sdkmcp.CallToolRequest, params *SearchServersParams,
) (*sdkmcp.CallToolResult, any, error) {
	startTime := time.Now()
	timeout := 25 * time.Second

	// Determine target limit
	targetLimit := params.Limit
	if targetLimit == 0 {
		targetLimit = 20 // single page by default (changed from 1000)
	}
	if targetLimit > 1000 {
		targetLimit = 1000 // safety cap
	}

	// Chunked fetching with timeout protection
	allServers := []upstreamv0.ServerResponse{}
	cursor := params.Cursor // Start from provided cursor (for agent iteration)
	pagesRead := 0
	truncated := false

	for {
		// Check timeout
		if time.Since(startTime) > timeout {
			logger.Warnf("Search timeout after %d pages, returning partial results", pagesRead)
			truncated = true
			break
		}

		// Build query parameters for this page
		queryParams := url.Values{}
		if cursor != "" {
			queryParams.Set("cursor", cursor)
		}
		if params.VersionFilter != "" {
			queryParams.Set("version", params.VersionFilter)
		}

		// Fetch page from Registry API
		page, err := s.listServersFromAPI(ctx, queryParams)
		if err != nil {
			if pagesRead == 0 {
				// First page failed - return error
				logger.Errorf("Failed to fetch servers from API: %v", err)
				return &sdkmcp.CallToolResult{
					Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("Error: failed to fetch servers: %v", err)}},
					IsError: true,
				}, nil, nil
			}
			// Subsequent page failed - return what we have
			logger.Warnf("Failed to fetch page %d, returning partial results: %v", pagesRead+1, err)
			truncated = true
			break
		}

		// Accumulate servers
		allServers = append(allServers, page.Servers...)
		pagesRead++

		// Check if we have enough
		if len(allServers) >= targetLimit {
			allServers = allServers[:targetLimit]
			// Save cursor for return (more results may be available)
			cursor = page.Metadata.NextCursor
			break
		}

		// Check if more pages exist
		if page.Metadata.NextCursor == "" {
			cursor = "" // No more pages
			break
		}
		cursor = page.Metadata.NextCursor
	}

	// Capture last cursor for agent iteration
	lastNextCursor := ""
	if cursor != "" {
		lastNextCursor = cursor // More results available
	}

	// Apply client-side filters
	filtered := s.applyFilters(allServers, params)

	// Apply sorting
	sorted := s.applySorting(filtered, params.SortBy)

	// Build response with extended metadata
	type ExtendedMetadata struct {
		Count       int    `json:"count"`
		NextCursor  string `json:"nextCursor,omitempty"` // Enable agent iteration
		Truncated   bool   `json:"truncated,omitempty"`
		PagesRead   int    `json:"pagesRead,omitempty"`
		TimeElapsed string `json:"timeElapsed,omitempty"`
	}

	extendedResp := struct {
		Servers  []upstreamv0.ServerResponse `json:"servers"`
		Metadata ExtendedMetadata            `json:"metadata"`
	}{
		Servers: sorted,
		Metadata: ExtendedMetadata{
			Count:       len(sorted),
			NextCursor:  lastNextCursor, // Return cursor for agent iteration
			Truncated:   truncated,
			PagesRead:   pagesRead,
			TimeElapsed: time.Since(startTime).Round(time.Millisecond).String(),
		},
	}

	// Return as JSON
	jsonBytes, err := json.MarshalIndent(extendedResp, "", "  ")
	if err != nil {
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("Error: failed to serialize response: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(jsonBytes)}},
	}, nil, nil
}

// applyFilters applies client-side filtering to server list
func (s *Server) applyFilters(servers []upstreamv0.ServerResponse, params *SearchServersParams) []upstreamv0.ServerResponse {
	filtered := []upstreamv0.ServerResponse{}

	for _, serverResp := range servers {
		server := serverResp.Server

		// Filter by name (substring match)
		if params.Name != "" {
			if !strings.Contains(strings.ToLower(server.Name), strings.ToLower(params.Name)) {
				continue
			}
		}

		// Filter by query (search in name, description)
		if params.Query != "" {
			queryLower := strings.ToLower(params.Query)
			matchFound := false

			if strings.Contains(strings.ToLower(server.Name), queryLower) {
				matchFound = true
			} else if strings.Contains(strings.ToLower(server.Description), queryLower) {
				matchFound = true
			} else {
				// Search in tools
				tools := extractTools(server)
				for _, tool := range tools {
					if strings.Contains(strings.ToLower(tool), queryLower) {
						matchFound = true
						break
					}
				}
			}

			if !matchFound {
				continue
			}
		}

		// Filter by tags
		if len(params.Tags) > 0 {
			serverTags := extractTags(server)
			hasAllTags := true
			for _, requiredTag := range params.Tags {
				found := false
				for _, serverTag := range serverTags {
					if strings.EqualFold(serverTag, requiredTag) {
						found = true
						break
					}
				}
				if !found {
					hasAllTags = false
					break
				}
			}
			if !hasAllTags {
				continue
			}
		}

		// Filter by tools
		if len(params.Tools) > 0 {
			serverTools := extractTools(server)
			hasAllTools := true
			for _, requiredTool := range params.Tools {
				found := false
				for _, serverTool := range serverTools {
					if strings.Contains(strings.ToLower(serverTool), strings.ToLower(requiredTool)) {
						found = true
						break
					}
				}
				if !found {
					hasAllTools = false
					break
				}
			}
			if !hasAllTools {
				continue
			}
		}

		// Filter by transport
		if params.Transport != "" {
			hasTransport := false
			for _, pkg := range server.Packages {
				if strings.EqualFold(pkg.Transport.Type, params.Transport) {
					hasTransport = true
					break
				}
			}
			if !hasTransport {
				continue
			}
		}

		// Filter by stars
		if params.MinStars > 0 {
			stars := extractStars(server)
			if stars < int64(params.MinStars) {
				continue
			}
		}

		// Filter by pulls
		if params.MinPulls > 0 {
			pulls := extractPulls(server)
			if pulls < int64(params.MinPulls) {
				continue
			}
		}

		// Filter by tier
		if params.Tier != "" {
			thMeta := extractToolHiveMetadata(server)
			tier, _ := thMeta["tier"].(string)
			if !strings.EqualFold(tier, params.Tier) {
				continue
			}
		}

		// Filter by status
		if params.Status != "" {
			thMeta := extractToolHiveMetadata(server)
			status, _ := thMeta["status"].(string)
			if !strings.EqualFold(status, params.Status) {
				continue
			}
		}

		// Server passed all filters
		filtered = append(filtered, serverResp)
	}

	return filtered
}

// extractTags extracts tags from server metadata
func extractTags(server upstreamv0.ServerJSON) []string {
	thMeta := extractToolHiveMetadata(server)
	if tags, ok := thMeta["tags"].([]any); ok {
		result := make([]string, 0, len(tags))
		for _, tag := range tags {
			if tagStr, ok := tag.(string); ok {
				result = append(result, tagStr)
			}
		}
		return result
	}
	return []string{}
}

// applySorting sorts servers based on the sort parameter
func (*Server) applySorting(servers []upstreamv0.ServerResponse, sortBy string) []upstreamv0.ServerResponse {
	if sortBy == "" {
		return servers
	}

	// Create a copy to avoid modifying the original
	sorted := make([]upstreamv0.ServerResponse, len(servers))
	copy(sorted, servers)

	switch sortBy {
	case "stars":
		sort.Slice(sorted, func(i, j int) bool {
			return extractStars(sorted[i].Server) > extractStars(sorted[j].Server)
		})
	case "pulls":
		sort.Slice(sorted, func(i, j int) bool {
			return extractPulls(sorted[i].Server) > extractPulls(sorted[j].Server)
		})
	case "name":
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Server.Name < sorted[j].Server.Name
		})
	case "updated_at":
		sort.Slice(sorted, func(_, _ int) bool {
			// TODO: Extract updated_at from metadata
			return false
		})
	}

	return sorted
}

// getServerDetails implements the get_server_details tool
func (s *Server) getServerDetails(
	ctx context.Context, _ *sdkmcp.CallToolRequest, params *GetServerDetailsParams,
) (*sdkmcp.CallToolResult, any, error) {
	// SDK validates required fields
	serverName := params.ServerName

	// Get server from Registry API
	server, err := s.getServerFromAPI(ctx, serverName)
	if err != nil {
		logger.Errorf("Failed to get server %s from API: %v", serverName, err)
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("Error: server not found: %s", serverName)}},
			IsError: true,
		}, nil, nil
	}

	// Create ServerResponse in official format
	serverResp := upstreamv0.ServerResponse{
		Server: server,
	}

	// Return the official ServerResponse format as JSON
	jsonBytes, err := json.MarshalIndent(serverResp, "", "  ")
	if err != nil {
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("Error: failed to serialize response: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(jsonBytes)}},
	}, nil, nil
}

// listServers implements the list_servers tool
// compareServers implements the compare_servers tool
func (s *Server) compareServers(ctx context.Context, req *sdkmcp.CallToolRequest, params *CompareServersParams) (*sdkmcp.CallToolResult, any, error) {
	// SDK validates array length via jsonschema tags (minItems=2, maxItems=5)
	serverNames := params.ServerNames

	// Fetch all servers from Registry API
	servers := make([]upstreamv0.ServerJSON, 0, len(serverNames))
	for _, name := range serverNames {
		server, err := s.getServerFromAPI(ctx, name)
		if err != nil {
			logger.Errorf("Failed to get server %s from API: %v", name, err)
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("Error: server not found: %s", name)}},
				IsError: true,
			}, nil, nil
		}
		servers = append(servers, server)
	}

	// Format comparison
	var result strings.Builder
	result.WriteString("# Server Comparison\n\n")

	// Create comparison table
	result.WriteString("| Attribute | ")
	for _, server := range servers {
		result.WriteString(fmt.Sprintf("%s | ", server.Name))
	}
	result.WriteString("\n|-----------|")
	for range servers {
		result.WriteString("----------|")
	}
	result.WriteString("\n")

	// Version
	result.WriteString("| **Version** | ")
	for _, server := range servers {
		result.WriteString(fmt.Sprintf("%s | ", server.Version))
	}
	result.WriteString("\n")

	// Stars
	result.WriteString("| **⭐ Stars** | ")
	for _, server := range servers {
		result.WriteString(fmt.Sprintf("%d | ", extractStars(server)))
	}
	result.WriteString("\n")

	// Pulls
	result.WriteString("| **📦 Pulls** | ")
	for _, server := range servers {
		result.WriteString(fmt.Sprintf("%d | ", extractPulls(server)))
	}
	result.WriteString("\n")

	// Tools count
	result.WriteString("| **🔧 Tools** | ")
	for _, server := range servers {
		tools := extractTools(server)
		result.WriteString(fmt.Sprintf("%d | ", len(tools)))
	}
	result.WriteString("\n")

	// Transport
	result.WriteString("| **Transport** | ")
	for _, server := range servers {
		thMeta := extractToolHiveMetadata(server)
		transport := notAvailable
		if t, ok := thMeta["transport"].(string); ok {
			transport = t
		}
		result.WriteString(fmt.Sprintf("%s | ", transport))
	}
	result.WriteString("\n")

	// Tier
	result.WriteString("| **Tier** | ")
	for _, server := range servers {
		thMeta := extractToolHiveMetadata(server)
		tier := notAvailable
		if t, ok := thMeta["tier"].(string); ok {
			tier = t
		}
		result.WriteString(fmt.Sprintf("%s | ", tier))
	}
	result.WriteString("\n")

	// Status
	result.WriteString("| **Status** | ")
	for _, server := range servers {
		thMeta := extractToolHiveMetadata(server)
		status := notAvailable
		if s, ok := thMeta["status"].(string); ok {
			status = s
		}
		result.WriteString(fmt.Sprintf("%s | ", status))
	}
	result.WriteString("\n\n")

	// Detailed descriptions
	result.WriteString("## Descriptions\n\n")
	for _, server := range servers {
		result.WriteString(fmt.Sprintf("### %s\n", server.Name))
		result.WriteString(fmt.Sprintf("%s\n\n", server.Description))
	}

	// Tool lists
	result.WriteString("## Available Tools\n\n")
	for _, server := range servers {
		tools := extractTools(server)
		result.WriteString(fmt.Sprintf("### %s\n", server.Name))
		if len(tools) > 0 {
			for _, tool := range tools {
				result.WriteString(fmt.Sprintf("- %s\n", tool))
			}
		} else {
			result.WriteString("No tool information available\n")
		}
		result.WriteString("\n")
	}

	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: result.String()}},
	}, nil, nil
}

// HTTP API helper methods

// listServersFromAPI fetches servers from the Registry API or local cache with pagination support
func (s *Server) listServersFromAPI(ctx context.Context, queryParams url.Values) (*upstreamv0.ServerListResponse, error) {
	// Use local cache if available (integrated mode)
	if s.localCache != nil {
		servers, err := s.localCache.ListServers(ctx)
		if err != nil {
			return nil, err
		}
		// Convert to ServerListResponse format
		serverResponses := make([]upstreamv0.ServerResponse, len(servers))
		for i, srv := range servers {
			serverResponses[i] = upstreamv0.ServerResponse{
				Server: srv,
			}
		}
		return &upstreamv0.ServerListResponse{
			Servers: serverResponses,
			Metadata: upstreamv0.Metadata{
				Count: len(serverResponses),
			},
		}, nil
	}

	// Otherwise call Registry API (standalone mode)
	reqURL := fmt.Sprintf("%s/v0/servers", s.apiClient.BaseURL)
	if len(queryParams) > 0 {
		reqURL += "?" + queryParams.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.apiClient.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Registry API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the official ServerListResponse format
	var listResp upstreamv0.ServerListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &listResp, nil
}

// getServerFromAPI fetches a specific server from the Registry API or local cache
func (s *Server) getServerFromAPI(ctx context.Context, serverName string) (upstreamv0.ServerJSON, error) {
	// Use local cache if available (integrated mode)
	if s.localCache != nil {
		return s.localCache.GetServer(ctx, serverName)
	}

	// Try direct get endpoint first (for compatible registries)
	encodedName := url.PathEscape(serverName)
	reqURL := fmt.Sprintf("%s/v0/servers/%s", s.apiClient.BaseURL, encodedName)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return upstreamv0.ServerJSON{}, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.apiClient.HTTPClient.Do(req)
	if err != nil {
		return upstreamv0.ServerJSON{}, fmt.Errorf("failed to call Registry API: %w", err)
	}
	defer resp.Body.Close()

	// If endpoint exists and returns success
	if resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return upstreamv0.ServerJSON{}, fmt.Errorf("failed to read response: %w", err)
		}

		// Try official registry format first: { "server": {...} }
		var officialFormat struct {
			Server upstreamv0.ServerJSON `json:"server"`
		}
		if err := json.Unmarshal(body, &officialFormat); err == nil && officialFormat.Server.Name != "" {
			return officialFormat.Server, nil
		}

		// Try direct server format: {...}
		var server upstreamv0.ServerJSON
		if err := json.Unmarshal(body, &server); err != nil {
			return upstreamv0.ServerJSON{}, fmt.Errorf("failed to decode response (tried both formats): %w", err)
		}
		return server, nil
	}

	// If endpoint doesn't exist (404), fall back to listing all servers and filtering
	// This handles registries like the official MCP registry that don't have a get-by-name endpoint
	if resp.StatusCode == http.StatusNotFound {
		listResp, err := s.listServersFromAPI(ctx, url.Values{})
		if err != nil {
			return upstreamv0.ServerJSON{}, fmt.Errorf("failed to list servers: %w", err)
		}

		// Find the server by name
		for _, serverResp := range listResp.Servers {
			if serverResp.Server.Name == serverName {
				return serverResp.Server, nil
			}
		}
		return upstreamv0.ServerJSON{}, fmt.Errorf("server not found: %s", serverName)
	}

	// Other error
	body, _ := io.ReadAll(resp.Body)
	return upstreamv0.ServerJSON{}, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
}
