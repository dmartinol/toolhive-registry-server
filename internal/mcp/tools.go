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
	Query        string   `json:"query,omitempty" jsonschema:"Natural language query or keywords"`
	Name         string   `json:"name,omitempty" jsonschema:"Filter by server name (substring match)"`
	Tags         []string `json:"tags,omitempty" jsonschema:"Filter by tags"`
	Tools        []string `json:"tools,omitempty" jsonschema:"Filter by tool names"`
	Transport    string   `json:"transport,omitempty" jsonschema:"Filter by transport type (stdio, http, sse)"`
	RegistryType string   `json:"registry_type,omitempty" jsonschema:"Filter by registry type (npm, pypi, oci, nuget, mcpb)"`

	// Metadata Filters
	MinStars int    `json:"min_stars,omitempty" jsonschema:"Minimum star count"`
	MinPulls int    `json:"min_pulls,omitempty" jsonschema:"Minimum pull count"`
	Tier     string `json:"tier,omitempty" jsonschema:"Filter by tier"`
	Status   string `json:"status,omitempty" jsonschema:"Filter by status"`

	// Pagination Control
	Cursor        string `json:"cursor,omitempty" jsonschema:"Pagination cursor from previous response (for iterating)"`
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
		if s.matchesAllFilters(serverResp.Server, params) {
			filtered = append(filtered, serverResp)
		}
	}

	return filtered
}

// matchesAllFilters checks if a server matches all filter criteria
func (s *Server) matchesAllFilters(server upstreamv0.ServerJSON, params *SearchServersParams) bool {
	return s.matchesNameFilter(server, params.Name) &&
		s.matchesQueryFilter(server, params.Query) &&
		s.matchesTagsFilter(server, params.Tags) &&
		s.matchesToolsFilter(server, params.Tools) &&
		s.matchesTransportFilter(server, params.Transport) &&
		s.matchesRegistryTypeFilter(server, params.RegistryType) &&
		s.matchesStarsFilter(server, params.MinStars) &&
		s.matchesPullsFilter(server, params.MinPulls) &&
		s.matchesTierFilter(server, params.Tier) &&
		s.matchesStatusFilter(server, params.Status)
}

// matchesNameFilter checks if server name contains the filter string
func (*Server) matchesNameFilter(server upstreamv0.ServerJSON, nameFilter string) bool {
	if nameFilter == "" {
		return true
	}
	return strings.Contains(strings.ToLower(server.Name), strings.ToLower(nameFilter))
}

// matchesQueryFilter checks if query matches name, description, or tools
func (*Server) matchesQueryFilter(server upstreamv0.ServerJSON, query string) bool {
	if query == "" {
		return true
	}

	queryLower := strings.ToLower(query)

	// Check name
	if strings.Contains(strings.ToLower(server.Name), queryLower) {
		return true
	}

	// Check description
	if strings.Contains(strings.ToLower(server.Description), queryLower) {
		return true
	}

	// Check tools
	tools := extractTools(server)
	for _, tool := range tools {
		if strings.Contains(strings.ToLower(tool), queryLower) {
			return true
		}
	}

	return false
}

// matchesTagsFilter checks if server has all required tags
func (s *Server) matchesTagsFilter(server upstreamv0.ServerJSON, requiredTags []string) bool {
	if len(requiredTags) == 0 {
		return true
	}

	serverTags := extractTags(server)
	for _, requiredTag := range requiredTags {
		if !s.hasTag(serverTags, requiredTag) {
			return false
		}
	}
	return true
}

// hasTag checks if a tag exists in the tags list (case-insensitive)
func (*Server) hasTag(tags []string, targetTag string) bool {
	for _, tag := range tags {
		if strings.EqualFold(tag, targetTag) {
			return true
		}
	}
	return false
}

// matchesToolsFilter checks if server has all required tools
func (s *Server) matchesToolsFilter(server upstreamv0.ServerJSON, requiredTools []string) bool {
	if len(requiredTools) == 0 {
		return true
	}

	serverTools := extractTools(server)
	for _, requiredTool := range requiredTools {
		if !s.hasTool(serverTools, requiredTool) {
			return false
		}
	}
	return true
}

// hasTool checks if a tool exists in the tools list (case-insensitive substring match)
func (*Server) hasTool(tools []string, targetTool string) bool {
	targetLower := strings.ToLower(targetTool)
	for _, tool := range tools {
		if strings.Contains(strings.ToLower(tool), targetLower) {
			return true
		}
	}
	return false
}

// matchesTransportFilter checks if server supports the transport type
func (*Server) matchesTransportFilter(server upstreamv0.ServerJSON, transport string) bool {
	if transport == "" {
		return true
	}

	for _, pkg := range server.Packages {
		if strings.EqualFold(pkg.Transport.Type, transport) {
			return true
		}
	}
	return false
}

// matchesRegistryTypeFilter checks if server has packages with the specified registry type
func (*Server) matchesRegistryTypeFilter(server upstreamv0.ServerJSON, registryType string) bool {
	if registryType == "" {
		return true
	}

	for _, pkg := range server.Packages {
		if strings.EqualFold(pkg.RegistryType, registryType) {
			return true
		}
	}
	return false
}

// matchesStarsFilter checks if server meets minimum star count
func (*Server) matchesStarsFilter(server upstreamv0.ServerJSON, minStars int) bool {
	if minStars <= 0 {
		return true
	}
	return extractStars(server) >= int64(minStars)
}

// matchesPullsFilter checks if server meets minimum pull count
func (*Server) matchesPullsFilter(server upstreamv0.ServerJSON, minPulls int) bool {
	if minPulls <= 0 {
		return true
	}
	return extractPulls(server) >= int64(minPulls)
}

// matchesTierFilter checks if server matches the tier
func (*Server) matchesTierFilter(server upstreamv0.ServerJSON, tier string) bool {
	if tier == "" {
		return true
	}
	thMeta := extractToolHiveMetadata(server)
	serverTier, _ := thMeta["tier"].(string)
	return strings.EqualFold(serverTier, tier)
}

// matchesStatusFilter checks if server matches the status
func (*Server) matchesStatusFilter(server upstreamv0.ServerJSON, status string) bool {
	if status == "" {
		return true
	}
	thMeta := extractToolHiveMetadata(server)
	serverStatus, _ := thMeta["status"].(string)
	return strings.EqualFold(serverStatus, status)
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

// compareServers implements the compare_servers tool
func (s *Server) compareServers(
	ctx context.Context, _ *sdkmcp.CallToolRequest, params *CompareServersParams,
) (*sdkmcp.CallToolResult, any, error) {
	// SDK validates array length via jsonschema tags (minItems=2, maxItems=5)
	serverNames := params.ServerNames

	// Fetch all servers from Registry API
	servers, err := s.fetchServersForComparison(ctx, serverNames)
	if err != nil {
		return err, nil, nil
	}

	// Build comparison output
	result := s.buildComparisonOutput(servers)

	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: result}},
	}, nil, nil
}

// fetchServersForComparison retrieves all servers by name for comparison
func (s *Server) fetchServersForComparison(
	ctx context.Context, serverNames []string,
) ([]upstreamv0.ServerJSON, *sdkmcp.CallToolResult) {
	servers := make([]upstreamv0.ServerJSON, 0, len(serverNames))
	for _, name := range serverNames {
		server, err := s.getServerFromAPI(ctx, name)
		if err != nil {
			logger.Errorf("Failed to get server %s from API: %v", name, err)
			return nil, &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("Error: server not found: %s", name)}},
				IsError: true,
			}
		}
		servers = append(servers, server)
	}
	return servers, nil
}

// buildComparisonOutput generates the full comparison markdown output
func (s *Server) buildComparisonOutput(servers []upstreamv0.ServerJSON) string {
	var result strings.Builder
	result.WriteString("# Server Comparison\n\n")

	s.writeComparisonTable(&result, servers)
	s.writeDescriptions(&result, servers)
	s.writeToolLists(&result, servers)

	return result.String()
}

// writeComparisonTable writes the comparison table with all attributes
func (s *Server) writeComparisonTable(result *strings.Builder, servers []upstreamv0.ServerJSON) {
	// Table header
	s.writeTableHeader(result, servers)

	// Define attribute rows
	attributes := []struct {
		label     string
		extractor func(upstreamv0.ServerJSON) string
	}{
		{"**Version**", func(srv upstreamv0.ServerJSON) string { return srv.Version }},
		{"**⭐ Stars**", func(srv upstreamv0.ServerJSON) string { return fmt.Sprintf("%d", extractStars(srv)) }},
		{"**📦 Pulls**", func(srv upstreamv0.ServerJSON) string { return fmt.Sprintf("%d", extractPulls(srv)) }},
		{"**🔧 Tools**", func(srv upstreamv0.ServerJSON) string { return fmt.Sprintf("%d", len(extractTools(srv))) }},
		{"**Transport**", s.extractTransportValue},
		{"**Tier**", s.extractTierValue},
		{"**Status**", s.extractStatusValue},
	}

	// Write each attribute row
	for _, attr := range attributes {
		s.writeTableRow(result, servers, attr.label, attr.extractor)
	}

	result.WriteString("\n")
}

// writeTableHeader writes the table header and separator
func (*Server) writeTableHeader(result *strings.Builder, servers []upstreamv0.ServerJSON) {
	result.WriteString("| Attribute | ")
	for _, server := range servers {
		fmt.Fprintf(result, "%s | ", server.Name)
	}
	result.WriteString("\n|-----------|")
	for range servers {
		result.WriteString("----------|")
	}
	result.WriteString("\n")
}

// writeTableRow writes a single attribute row in the comparison table
func (*Server) writeTableRow(
	result *strings.Builder, servers []upstreamv0.ServerJSON, label string,
	extractor func(upstreamv0.ServerJSON) string,
) {
	fmt.Fprintf(result, "| %s | ", label)
	for _, server := range servers {
		fmt.Fprintf(result, "%s | ", extractor(server))
	}
	result.WriteString("\n")
}

// extractTransportValue extracts transport from ToolHive metadata
func (*Server) extractTransportValue(server upstreamv0.ServerJSON) string {
	thMeta := extractToolHiveMetadata(server)
	if transport, ok := thMeta["transport"].(string); ok {
		return transport
	}
	return notAvailable
}

// extractTierValue extracts tier from ToolHive metadata
func (*Server) extractTierValue(server upstreamv0.ServerJSON) string {
	thMeta := extractToolHiveMetadata(server)
	if tier, ok := thMeta["tier"].(string); ok {
		return tier
	}
	return notAvailable
}

// extractStatusValue extracts status from ToolHive metadata
func (*Server) extractStatusValue(server upstreamv0.ServerJSON) string {
	thMeta := extractToolHiveMetadata(server)
	if status, ok := thMeta["status"].(string); ok {
		return status
	}
	return notAvailable
}

// writeDescriptions writes the descriptions section
func (*Server) writeDescriptions(result *strings.Builder, servers []upstreamv0.ServerJSON) {
	result.WriteString("## Descriptions\n\n")
	for _, server := range servers {
		fmt.Fprintf(result, "### %s\n", server.Name)
		fmt.Fprintf(result, "%s\n\n", server.Description)
	}
}

// writeToolLists writes the available tools section
func (*Server) writeToolLists(result *strings.Builder, servers []upstreamv0.ServerJSON) {
	result.WriteString("## Available Tools\n\n")
	for _, server := range servers {
		tools := extractTools(server)
		fmt.Fprintf(result, "### %s\n", server.Name)
		if len(tools) > 0 {
			for _, tool := range tools {
				fmt.Fprintf(result, "- %s\n", tool)
			}
		} else {
			result.WriteString("No tool information available\n")
		}
		result.WriteString("\n")
	}
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

	// Use the official MCP Registry API endpoint: /v0/servers/{name}/versions/latest
	encodedName := url.PathEscape(serverName)
	reqURL := fmt.Sprintf("%s/v0/servers/%s/versions/latest", s.apiClient.BaseURL, encodedName)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return upstreamv0.ServerJSON{}, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.apiClient.HTTPClient.Do(req)
	if err != nil {
		return upstreamv0.ServerJSON{}, fmt.Errorf("failed to call Registry API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return upstreamv0.ServerJSON{}, fmt.Errorf("failed to read response: %w", err)
	}

	// If endpoint returns success
	if resp.StatusCode == http.StatusOK {
		// Official registry format: { "server": {...}, "_meta": {...} }
		var officialFormat struct {
			Server upstreamv0.ServerJSON `json:"server"`
		}
		if err := json.Unmarshal(body, &officialFormat); err == nil && officialFormat.Server.Name != "" {
			return officialFormat.Server, nil
		}

		// Try direct server format: {...} (for backwards compatibility)
		var server upstreamv0.ServerJSON
		if err := json.Unmarshal(body, &server); err != nil {
			return upstreamv0.ServerJSON{}, fmt.Errorf("failed to decode response (tried both formats): %w", err)
		}
		return server, nil
	}

	// Server not found or other error
	return upstreamv0.ServerJSON{}, fmt.Errorf("server not found: %s (API returned status %d: %s)",
		serverName, resp.StatusCode, string(body))
}
