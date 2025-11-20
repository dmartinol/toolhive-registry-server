// Package mcp provides MCP (Model Context Protocol) server implementation
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/filtering"
	"github.com/stacklok/toolhive-registry-server/internal/registry"
)

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

// extractLastUpdated extracts last_updated timestamp from ToolHive metadata
func extractLastUpdated(server upstreamv0.ServerJSON) string {
	thMeta := extractToolHiveMetadata(server)
	if meta, ok := thMeta["metadata"].(map[string]any); ok {
		if updated, ok := meta["last_updated"].(string); ok {
			return updated
		}
	}
	return ""
}

// Tool handler implementations

// handleSearchServers implements the search_servers tool
func (s *Server) handleSearchServers(ctx context.Context, args map[string]any) (CallToolResult, error) {
	// Parse arguments
	query, _ := args["query"].(string)
	if query == "" {
		return CallToolResult{
			Content: []Content{{Type: "text", Text: "Error: query parameter is required"}},
			IsError: true,
		}, nil
	}

	limit := 10
	if limitFloat, ok := args["limit"].(float64); ok {
		limit = int(limitFloat)
	}

	var tags []string
	if tagsAny, ok := args["tags"].([]any); ok {
		tags = make([]string, 0, len(tagsAny))
		for _, tag := range tagsAny {
			if tagStr, ok := tag.(string); ok {
				tags = append(tags, tagStr)
			}
		}
	}

	// Get all servers from registry
	servers, err := s.service.ListServers(ctx)
	if err != nil {
		logger.Errorf("Failed to list servers: %v", err)
		return CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: failed to list servers: %v", err)}},
			IsError: true,
		}, nil
	}

	// Filter by tags if provided
	if len(tags) > 0 {
		tagFilter := filtering.NewDefaultTagFilter()
		filtered := make([]upstreamv0.ServerJSON, 0)
		for _, server := range servers {
			serverTags := registry.ExtractTags(&server)
			shouldInclude, _ := tagFilter.ShouldInclude(serverTags, tags, []string{})
			if shouldInclude {
				filtered = append(filtered, server)
			}
		}
		servers = filtered
	}

	// Filter by query (search in name, description, and tools)
	queryLower := strings.ToLower(query)
	filtered := make([]upstreamv0.ServerJSON, 0)
	for _, server := range servers {
		// Search in name
		if strings.Contains(strings.ToLower(server.Name), queryLower) {
			filtered = append(filtered, server)
			continue
		}

		// Search in description
		if strings.Contains(strings.ToLower(server.Description), queryLower) {
			filtered = append(filtered, server)
			continue
		}

		// Search in tools
		tools := extractTools(server)
		for _, tool := range tools {
			if strings.Contains(strings.ToLower(tool), queryLower) {
				filtered = append(filtered, server)
				break
			}
		}
	}

	// Sort by stars (descending)
	sort.Slice(filtered, func(i, j int) bool {
		return extractStars(filtered[i]) > extractStars(filtered[j])
	})

	// Limit results
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	// Format results
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Found %d servers matching '%s':\n\n", len(filtered), query))

	for i, server := range filtered {
		stars := extractStars(server)
		pulls := extractPulls(server)
		tools := extractTools(server)

		result.WriteString(fmt.Sprintf("%d. **%s** (v%s)\n", i+1, server.Name, server.Version))
		result.WriteString(fmt.Sprintf("   %s\n", server.Description))
		result.WriteString(fmt.Sprintf("   ⭐ Stars: %d | 📦 Pulls: %d\n", stars, pulls))

		if len(tools) > 0 {
			result.WriteString(fmt.Sprintf("   🔧 Tools: %s\n", strings.Join(tools, ", ")))
		}

		result.WriteString("\n")
	}

	if len(filtered) == 0 {
		result.WriteString("No servers found matching your query. Try different keywords or remove tag filters.")
	}

	return CallToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// handleGetServerDetails implements the get_server_details tool
func (s *Server) handleGetServerDetails(ctx context.Context, args map[string]any) (CallToolResult, error) {
	// Parse arguments
	serverName, _ := args["server_name"].(string)
	if serverName == "" {
		return CallToolResult{
			Content: []Content{{Type: "text", Text: "Error: server_name parameter is required"}},
			IsError: true,
		}, nil
	}

	version, _ := args["version"].(string)
	if version == "" {
		version = "latest"
	}

	// Get server from registry
	server, err := s.service.GetServer(ctx, serverName)
	if err != nil {
		logger.Errorf("Failed to get server %s: %v", serverName, err)
		return CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: server not found: %s", serverName)}},
			IsError: true,
		}, nil
	}

	// Extract metadata
	stars := extractStars(server)
	pulls := extractPulls(server)
	tools := extractTools(server)
	lastUpdated := extractLastUpdated(server)
	thMeta := extractToolHiveMetadata(server)

	// Format result
	var result strings.Builder
	result.WriteString(fmt.Sprintf("# %s\n\n", server.Name))
	result.WriteString(fmt.Sprintf("**Version:** %s\n", server.Version))
	result.WriteString(fmt.Sprintf("**Description:** %s\n\n", server.Description))

	// Statistics
	result.WriteString("## Statistics\n")
	result.WriteString(fmt.Sprintf("- ⭐ Stars: %d\n", stars))
	result.WriteString(fmt.Sprintf("- 📦 Pulls: %d\n", pulls))
	if lastUpdated != "" {
		result.WriteString(fmt.Sprintf("- 🕒 Last Updated: %s\n", lastUpdated))
	}
	result.WriteString("\n")

	// Tools
	if len(tools) > 0 {
		result.WriteString("## Available Tools\n")
		for _, tool := range tools {
			result.WriteString(fmt.Sprintf("- %s\n", tool))
		}
		result.WriteString("\n")
	}

	// Metadata
	if tier, ok := thMeta["tier"].(string); ok {
		result.WriteString(fmt.Sprintf("**Tier:** %s\n", tier))
	}
	if status, ok := thMeta["status"].(string); ok {
		result.WriteString(fmt.Sprintf("**Status:** %s\n", status))
	}
	if transport, ok := thMeta["transport"].(string); ok {
		result.WriteString(fmt.Sprintf("**Transport:** %s\n", transport))
	}
	result.WriteString("\n")

	// Repository
	if server.Repository != nil {
		result.WriteString("## Repository\n")
		result.WriteString(fmt.Sprintf("- URL: %s\n", server.Repository.URL))
		if server.Repository.Source != "" {
			result.WriteString(fmt.Sprintf("- Source: %s\n", server.Repository.Source))
		}
		result.WriteString("\n")
	}

	// Packages
	if len(server.Packages) > 0 {
		result.WriteString("## Packages\n")
		for i, pkg := range server.Packages {
			result.WriteString(fmt.Sprintf("%d. **%s** (%s)\n", i+1, pkg.RegistryType, pkg.Identifier))
			if pkg.RunTimeHint != "" {
				result.WriteString(fmt.Sprintf("   Runtime: %s\n", pkg.RunTimeHint))
			}
			if pkg.Transport.Type != "" {
				result.WriteString(fmt.Sprintf("   Transport: %s\n", pkg.Transport.Type))
			}
		}
		result.WriteString("\n")
	}

	// Tags
	tags := registry.ExtractTags(&server)
	if len(tags) > 0 {
		result.WriteString(fmt.Sprintf("**Tags:** %s\n", strings.Join(tags, ", ")))
	}

	return CallToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// handleListServers implements the list_servers tool
func (s *Server) handleListServers(ctx context.Context, args map[string]any) (CallToolResult, error) {
	// Parse arguments
	limit := 20
	if limitFloat, ok := args["limit"].(float64); ok {
		limit = int(limitFloat)
	}

	sortBy, _ := args["sort_by"].(string)
	if sortBy == "" {
		sortBy = "stars"
	}

	// Get all servers from registry
	servers, err := s.service.ListServers(ctx)
	if err != nil {
		logger.Errorf("Failed to list servers: %v", err)
		return CallToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: failed to list servers: %v", err)}},
			IsError: true,
		}, nil
	}

	// Sort servers
	switch sortBy {
	case "stars":
		sort.Slice(servers, func(i, j int) bool {
			return extractStars(servers[i]) > extractStars(servers[j])
		})
	case "pulls":
		sort.Slice(servers, func(i, j int) bool {
			return extractPulls(servers[i]) > extractPulls(servers[j])
		})
	case "name":
		sort.Slice(servers, func(i, j int) bool {
			return servers[i].Name < servers[j].Name
		})
	case "updated_at":
		sort.Slice(servers, func(i, j int) bool {
			return extractLastUpdated(servers[i]) > extractLastUpdated(servers[j])
		})
	}

	// Limit results
	if len(servers) > limit {
		servers = servers[:limit]
	}

	// Format results
	var result strings.Builder
	result.WriteString(fmt.Sprintf("# Available MCP Servers (Total: %d, Showing: %d)\n\n", len(servers), limit))
	result.WriteString(fmt.Sprintf("Sorted by: %s\n\n", sortBy))

	for i, server := range servers {
		stars := extractStars(server)
		pulls := extractPulls(server)

		result.WriteString(fmt.Sprintf("%d. **%s** (v%s)\n", i+1, server.Name, server.Version))
		result.WriteString(fmt.Sprintf("   %s\n", server.Description))
		result.WriteString(fmt.Sprintf("   ⭐ %d | 📦 %d\n\n", stars, pulls))
	}

	return CallToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// handleCompareServers implements the compare_servers tool
func (s *Server) handleCompareServers(ctx context.Context, args map[string]any) (CallToolResult, error) {
	// Parse arguments
	serverNamesAny, ok := args["server_names"].([]any)
	if !ok || len(serverNamesAny) < 2 {
		return CallToolResult{
			Content: []Content{{Type: "text", Text: "Error: server_names parameter is required and must contain at least 2 servers"}},
			IsError: true,
		}, nil
	}

	if len(serverNamesAny) > 5 {
		return CallToolResult{
			Content: []Content{{Type: "text", Text: "Error: maximum 5 servers can be compared at once"}},
			IsError: true,
		}, nil
	}

	serverNames := make([]string, 0, len(serverNamesAny))
	for _, name := range serverNamesAny {
		if nameStr, ok := name.(string); ok {
			serverNames = append(serverNames, nameStr)
		}
	}

	// Fetch all servers
	servers := make([]upstreamv0.ServerJSON, 0, len(serverNames))
	for _, name := range serverNames {
		server, err := s.service.GetServer(ctx, name)
		if err != nil {
			logger.Errorf("Failed to get server %s: %v", name, err)
			return CallToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error: server not found: %s", name)}},
				IsError: true,
			}, nil
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
		transport := "N/A"
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
		tier := "N/A"
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
		status := "N/A"
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

	return CallToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// Helper to format JSON for display
func formatJSON(data any) string {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error formatting JSON: %v", err)
	}
	return string(jsonBytes)
}
