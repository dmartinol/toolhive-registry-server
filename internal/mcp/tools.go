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

	// Registry types
	registryTypeNPM     = "npm"
	registryTypePyPI    = "pypi"
	registryTypeDocker  = "docker"
	registryTypeUnknown = "unknown"
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

// GetSetupGuideParams defines parameters for the get_setup_guide tool
type GetSetupGuideParams struct {
	ServerName string `json:"server_name" jsonschema:"required,Server to get setup guide for"`
	Platform   string `json:"platform,omitempty" jsonschema:"Platform: claude-desktop, cursor, custom (default: claude-desktop)"`
	Runtime    string `json:"runtime,omitempty" jsonschema:"Runtime: node, python, docker (auto-detected if not specified)"`
}

// FindAlternativesParams defines parameters for the find_alternatives tool
type FindAlternativesParams struct {
	ServerName string `json:"server_name" jsonschema:"required,Find alternatives to this server"`
	Reason     string `json:"reason,omitempty" jsonschema:"Why looking for alternative: deprecated, license, features, performance"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Max alternatives (default: 5, max: 20)"`
}

// Journey 2: MCP Developer Tools

// FindSimilarServersParams defines parameters for the find_similar_servers tool
type FindSimilarServersParams struct {
	ServerName string   `json:"server_name,omitempty" jsonschema:"Find servers similar to this one"`
	Tags       []string `json:"tags,omitempty" jsonschema:"Find servers with these tags"`
	Tools      []string `json:"tools,omitempty" jsonschema:"Find servers with these tools"`
	Limit      int      `json:"limit,omitempty" jsonschema:"Max results (default: 10, max: 50)"`
}

// GetServerAnalyticsParams defines parameters for the get_server_analytics tool
type GetServerAnalyticsParams struct {
	ServerName string `json:"server_name" jsonschema:"required,Server to analyze"`
	Period     string `json:"period,omitempty" jsonschema:"Time period: 7d, 30d, 90d, all (default: 30d)"`
}

// GetEcosystemInsightsParams defines parameters for the get_ecosystem_insights tool
type GetEcosystemInsightsParams struct {
	Category string `json:"category,omitempty" jsonschema:"Category to analyze: database, files, api, all (default: all)"`
}

// AnalyzeToolOverlapParams defines parameters for the analyze_tool_overlap tool
type AnalyzeToolOverlapParams struct {
	ServerNames []string `json:"server_names" jsonschema:"required,Servers to analyze (2-10 servers)"`
	ShowUnique  bool     `json:"show_unique,omitempty" jsonschema:"Show unique tools per server (default: true)"`
}

// EnvVar represents an environment variable requirement
type EnvVar struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Example     string `json:"example"`
	Source      string `json:"source"` // "metadata", "derived", "convention"
}

// FreqItem represents a frequency count for ecosystem insights
type FreqItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
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
		if params.Name != "" {
			queryParams.Set("search", params.Name)
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

// Setup guide helper functions

// detectRuntime detects the runtime from server packages
func detectRuntime(server upstreamv0.ServerJSON) string {
	if len(server.Packages) == 0 {
		return registryTypeUnknown
	}

	pkg := server.Packages[0]
	switch pkg.RegistryType {
	case registryTypeNPM:
		return "node"
	case registryTypePyPI:
		return "python"
	case registryTypeDocker:
		return registryTypeDocker
	default:
		if pkg.RunTimeHint != "" {
			return pkg.RunTimeHint
		}
		return registryTypeUnknown
	}
}

// extractEnvironmentVariables extracts environment variables from server metadata
func extractEnvironmentVariables(server upstreamv0.ServerJSON) []EnvVar {
	seen := make(map[string]bool)

	// Extract from ToolHive metadata if available
	envVars := extractEnvVarsFromMetadata(server, seen)

	// Add conventional env vars based on tags
	envVars = appendConventionalEnvVars(server, envVars, seen)

	return envVars
}

// extractEnvVarsFromMetadata extracts environment variables from ToolHive metadata
func extractEnvVarsFromMetadata(server upstreamv0.ServerJSON, seen map[string]bool) []EnvVar {
	envVars := []EnvVar{}

	thMeta := extractToolHiveMetadata(server)
	envConfig, ok := thMeta["env"].(map[string]any)
	if !ok {
		return envVars
	}

	for name, config := range envConfig {
		if seen[name] {
			continue
		}
		envVar := parseEnvVarConfig(name, config)
		envVars = append(envVars, envVar)
		seen[name] = true
	}

	return envVars
}

// parseEnvVarConfig parses env var configuration from metadata
func parseEnvVarConfig(name string, config any) EnvVar {
	envVar := EnvVar{
		Name:   name,
		Source: "metadata",
	}

	cfgMap, ok := config.(map[string]any)
	if !ok {
		return envVar
	}

	if desc, ok := cfgMap["description"].(string); ok {
		envVar.Description = desc
	}
	if req, ok := cfgMap["required"].(bool); ok {
		envVar.Required = req
	}
	if ex, ok := cfgMap["example"].(string); ok {
		envVar.Example = ex
	}

	return envVar
}

// appendConventionalEnvVars adds conventional environment variables based on tags
func appendConventionalEnvVars(server upstreamv0.ServerJSON, envVars []EnvVar, seen map[string]bool) []EnvVar {
	tags := extractTags(server)
	hasDatabase, hasAPI, hasFiles := categorizeTags(tags)

	if hasDatabase && !seen["DATABASE_URL"] {
		envVars = append(envVars, EnvVar{
			Name:        "DATABASE_URL",
			Description: "Database connection string",
			Required:    true,
			Example:     "postgresql://user:password@localhost:5432/dbname",
			Source:      "convention",
		})
	}

	if hasAPI && !seen["API_KEY"] {
		envVars = append(envVars, EnvVar{
			Name:        "API_KEY",
			Description: "API authentication key",
			Required:    true,
			Example:     "your-api-key-here",
			Source:      "convention",
		})
	}

	if hasFiles && !seen["ROOT_PATH"] {
		envVars = append(envVars, EnvVar{
			Name:        "ROOT_PATH",
			Description: "Root directory path for file operations",
			Required:    false,
			Example:     "/path/to/files",
			Source:      "convention",
		})
	}

	return envVars
}

// categorizeTags determines which conventional env vars are needed based on tags
func categorizeTags(tags []string) (hasDatabase, hasAPI, hasFiles bool) {
	for _, tag := range tags {
		switch strings.ToLower(tag) {
		case "database", "sql", "postgres", "mysql":
			hasDatabase = true
		case "api":
			hasAPI = true
		case "files", "filesystem":
			hasFiles = true
		}
	}
	return
}

// generateEnvFileExample generates an example .env file content
func generateEnvFileExample(envVars []EnvVar) string {
	if len(envVars) == 0 {
		return "# No environment variables required\n"
	}

	var result strings.Builder
	result.WriteString("# Environment Variables Configuration\n")
	result.WriteString("# Copy this to .env and fill in your values\n\n")

	for _, env := range envVars {
		if env.Description != "" {
			result.WriteString(fmt.Sprintf("# %s\n", env.Description))
		}
		if env.Required {
			result.WriteString("# Required: yes\n")
		}
		result.WriteString(fmt.Sprintf("%s=%s\n\n", env.Name, env.Example))
	}

	return result.String()
}

// generateInstallationSteps generates installation steps based on package info
func generateInstallationSteps(server upstreamv0.ServerJSON, _ string) string {
	var steps strings.Builder

	if len(server.Packages) == 0 {
		steps.WriteString("1. Clone the repository\n")
		if server.Repository != nil && server.Repository.URL != "" {
			steps.WriteString(fmt.Sprintf("   ```bash\n   git clone %s\n   ```\n\n", server.Repository.URL))
		}
		steps.WriteString("2. Follow the setup instructions in the repository README\n\n")
		return steps.String()
	}

	pkg := server.Packages[0]

	switch pkg.RegistryType {
	case registryTypeNPM:
		steps.WriteString("1. Install the package using npm:\n")
		steps.WriteString(fmt.Sprintf("   ```bash\n   npm install -g %s\n   ```\n\n", pkg.Identifier))
		steps.WriteString("2. Or use npx to run without installing:\n")
		steps.WriteString(fmt.Sprintf("   ```bash\n   npx %s\n   ```\n\n", pkg.Identifier))

	case registryTypePyPI:
		steps.WriteString("1. Install the package using pip:\n")
		steps.WriteString(fmt.Sprintf("   ```bash\n   pip install %s\n   ```\n\n", pkg.Identifier))
		steps.WriteString("2. Or use pipx for isolated installation:\n")
		steps.WriteString(fmt.Sprintf("   ```bash\n   pipx install %s\n   ```\n\n", pkg.Identifier))

	case registryTypeDocker:
		steps.WriteString("1. Pull the Docker image:\n")
		steps.WriteString(fmt.Sprintf("   ```bash\n   docker pull %s\n   ```\n\n", pkg.Identifier))
		steps.WriteString("2. Run the container:\n")
		steps.WriteString(fmt.Sprintf("   ```bash\n   docker run -it %s\n   ```\n\n", pkg.Identifier))

	default:
		steps.WriteString("1. Install the package:\n")
		steps.WriteString("   ```bash\n")
		steps.WriteString(fmt.Sprintf("   # Install %s\n", pkg.Identifier))
		steps.WriteString("   # See repository for installation instructions\n")
		steps.WriteString("   ```\n\n")
	}

	return steps.String()
}

// generatePlatformConfig generates platform-specific configuration
func generatePlatformConfig(server upstreamv0.ServerJSON, platform string) string {
	if len(server.Packages) == 0 {
		return "# Configuration not available - no package information\n"
	}

	pkg := server.Packages[0]
	var config strings.Builder

	switch platform {
	case "claude-desktop":
		config.WriteString("### Claude Desktop Configuration\n\n")
		config.WriteString("Add to `~/.config/claude/config.json` (macOS/Linux) ")
		config.WriteString("or `%APPDATA%\\Claude\\config.json` (Windows):\n\n")
		config.WriteString("```json\n{\n  \"mcpServers\": {\n")
		config.WriteString(fmt.Sprintf("    \"%s\": {\n", server.Name))

		switch pkg.RegistryType {
		case registryTypeNPM:
			config.WriteString("      \"command\": \"npx\",\n")
			config.WriteString(fmt.Sprintf("      \"args\": [\"%s\"]\n", pkg.Identifier))
		case registryTypePyPI:
			config.WriteString("      \"command\": \"python\",\n")
			config.WriteString(fmt.Sprintf("      \"args\": [\"-m\", \"%s\"]\n", pkg.Identifier))
		default:
			config.WriteString(fmt.Sprintf("      \"command\": \"%s\"\n", pkg.Identifier))
		}

		config.WriteString("    }\n  }\n}\n```\n\n")

	case "cursor":
		config.WriteString("### Cursor Configuration\n\n")
		config.WriteString("Add to `~/.cursor/mcp.json`:\n\n")
		config.WriteString("```json\n{\n  \"mcpServers\": {\n")
		config.WriteString(fmt.Sprintf("    \"%s\": {\n", server.Name))

		switch pkg.RegistryType {
		case registryTypeNPM:
			config.WriteString("      \"command\": \"npx\",\n")
			config.WriteString(fmt.Sprintf("      \"args\": [\"%s\"]\n", pkg.Identifier))
		case registryTypePyPI:
			config.WriteString("      \"command\": \"python\",\n")
			config.WriteString(fmt.Sprintf("      \"args\": [\"-m\", \"%s\"]\n", pkg.Identifier))
		default:
			config.WriteString(fmt.Sprintf("      \"command\": \"%s\"\n", pkg.Identifier))
		}

		config.WriteString("    }\n  }\n}\n```\n\n")

	case "custom":
		config.WriteString("### Custom MCP Client Configuration\n\n")
		config.WriteString("Connect using stdio transport:\n\n")
		config.WriteString("```bash\n")
		switch pkg.RegistryType {
		case "npm":
			config.WriteString(fmt.Sprintf("npx %s\n", pkg.Identifier))
		case "pypi":
			config.WriteString(fmt.Sprintf("python -m %s\n", pkg.Identifier))
		default:
			config.WriteString(fmt.Sprintf("%s\n", pkg.Identifier))
		}
		config.WriteString("```\n\n")

	default:
		config.WriteString("### Configuration\n\n")
		config.WriteString("See your MCP client documentation for configuration instructions.\n\n")
	}

	return config.String()
}

// generateTroubleshootingTips generates troubleshooting tips based on transport and runtime
func generateTroubleshootingTips(server upstreamv0.ServerJSON) string {
	var tips strings.Builder
	tips.WriteString("## Troubleshooting\n\n")

	if len(server.Packages) == 0 {
		tips.WriteString("- Check the repository README for setup instructions\n")
		tips.WriteString("- Verify all dependencies are installed\n\n")
		return tips.String()
	}

	pkg := server.Packages[0]

	// Runtime-specific tips
	switch pkg.RegistryType {
	case registryTypeNPM:
		tips.WriteString("**Common Issues:**\n\n")
		tips.WriteString("- **\"command not found\"**: Ensure Node.js is installed: `node --version`\n")
		tips.WriteString("- **Permission errors**: Use `npx` instead of global install, or fix npm permissions\n")
		tips.WriteString("- **Version conflicts**: Try `npm install -g " + pkg.Identifier + "@latest`\n\n")

	case registryTypePyPI:
		tips.WriteString("**Common Issues:**\n\n")
		tips.WriteString("- **\"module not found\"**: Ensure Python is installed: `python --version`\n")
		tips.WriteString("- **Permission errors**: Use `pipx` for isolated installation\n")
		tips.WriteString("- **Version conflicts**: Try using a virtual environment: `python -m venv .venv`\n\n")

	case registryTypeDocker:
		tips.WriteString("**Common Issues:**\n\n")
		tips.WriteString("- **\"docker: command not found\"**: Install Docker Desktop\n")
		tips.WriteString("- **Permission errors**: Ensure Docker daemon is running\n")
		tips.WriteString("- **Image pull errors**: Check internet connection and Docker Hub access\n\n")
	}

	// Transport-specific tips
	if pkg.Transport.Type == "stdio" {
		tips.WriteString("**stdio Transport:**\n\n")
		tips.WriteString("- Ensure the server binary is executable and in your PATH\n")
		tips.WriteString("- Check that the command doesn't require interactive input\n\n")
	}

	tips.WriteString("**Still having issues?**\n\n")
	if server.Repository != nil && server.Repository.URL != "" {
		tips.WriteString(fmt.Sprintf("- Check the [GitHub Issues](%s/issues) for known problems\n", server.Repository.URL))
		tips.WriteString(fmt.Sprintf("- Read the [documentation](%s#readme)\n", server.Repository.URL))
	}
	tips.WriteString("- Verify your MCP client is up to date\n\n")

	return tips.String()
}

// Similarity scoring helper functions for find_alternatives

// scoreTagOverlap calculates tag overlap score (0.0 to 1.0) using Jaccard similarity.
// Jaccard similarity = |A ∩ B| / |A ∪ B| (intersection over union)
// See: https://en.wikipedia.org/wiki/Jaccard_index
func scoreTagOverlap(tagsA, tagsB []string) float64 {
	if len(tagsA) == 0 || len(tagsB) == 0 {
		return 0.0
	}

	// Convert to lowercase for case-insensitive comparison
	setA := make(map[string]bool)
	for _, tag := range tagsA {
		setA[strings.ToLower(tag)] = true
	}

	matches := 0
	for _, tag := range tagsB {
		if setA[strings.ToLower(tag)] {
			matches++
		}
	}

	// Return Jaccard similarity: intersection / union
	union := len(tagsA) + len(tagsB) - matches
	if union == 0 {
		return 0.0
	}
	return float64(matches) / float64(union)
}

// scoreToolOverlap calculates tool overlap score (0.0 to 1.0) using Jaccard similarity.
// Jaccard similarity = |A ∩ B| / |A ∪ B| (intersection over union)
// See: https://en.wikipedia.org/wiki/Jaccard_index
func scoreToolOverlap(toolsA, toolsB []string) float64 {
	if len(toolsA) == 0 || len(toolsB) == 0 {
		return 0.0
	}

	// Convert to lowercase for case-insensitive comparison
	setA := make(map[string]bool)
	for _, tool := range toolsA {
		setA[strings.ToLower(tool)] = true
	}

	matches := 0
	for _, tool := range toolsB {
		if setA[strings.ToLower(tool)] {
			matches++
		}
	}

	// Return Jaccard similarity
	union := len(toolsA) + len(toolsB) - matches
	if union == 0 {
		return 0.0
	}
	return float64(matches) / float64(union)
}

// scoreDescriptionSimilarity calculates description keyword similarity (0.0 to 1.0) using overlap coefficient.
// Overlap coefficient = |A ∩ B| / min(|A|, |B|)
// See: https://en.wikipedia.org/wiki/Overlap_coefficient
func scoreDescriptionSimilarity(descA, descB string) float64 {
	// Simple keyword-based similarity
	wordsA := strings.Fields(strings.ToLower(descA))
	wordsB := strings.Fields(strings.ToLower(descB))

	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0.0
	}

	// Create word frequency maps
	freqA := make(map[string]int)
	for _, word := range wordsA {
		// Filter out common stop words
		if len(word) > 3 {
			freqA[word]++
		}
	}

	matches := 0
	for _, word := range wordsB {
		if len(word) > 3 && freqA[word] > 0 {
			matches++
			freqA[word]-- // Count each match only once
		}
	}

	// Return overlap coefficient
	minLen := len(wordsA)
	if len(wordsB) < minLen {
		minLen = len(wordsB)
	}
	if minLen == 0 {
		return 0.0
	}
	return float64(matches) / float64(minLen)
}

// scoreTransportCompatibility checks if transport types are compatible (0.0 or 1.0)
func scoreTransportCompatibility(serverA, serverB upstreamv0.ServerJSON) float64 {
	if len(serverA.Packages) == 0 || len(serverB.Packages) == 0 {
		return 0.5 // Unknown transport, neutral score
	}

	transportA := strings.ToLower(serverA.Packages[0].Transport.Type)
	transportB := strings.ToLower(serverB.Packages[0].Transport.Type)

	if transportA == transportB {
		return 1.0
	}
	return 0.0
}

// calculateSimilarityScore calculates overall similarity score (0.0 to 1.0)
func calculateSimilarityScore(sourceServer, targetServer upstreamv0.ServerJSON) float64 {
	// Don't compare a server to itself
	if sourceServer.Name == targetServer.Name {
		return 0.0
	}

	// Extract metadata
	sourceTags := extractTags(sourceServer)
	targetTags := extractTags(targetServer)
	sourceTools := extractTools(sourceServer)
	targetTools := extractTools(targetServer)

	// Calculate component scores
	tagScore := scoreTagOverlap(sourceTags, targetTags)
	toolScore := scoreToolOverlap(sourceTools, targetTools)
	descScore := scoreDescriptionSimilarity(sourceServer.Description, targetServer.Description)
	transportScore := scoreTransportCompatibility(sourceServer, targetServer)

	// Weighted combination (as per plan: tags 40%, tools 40%, transport 10%, description 10%)
	similarityScore := (tagScore * 0.4) + (toolScore * 0.4) + (transportScore * 0.1) + (descScore * 0.1)

	return similarityScore
}

// estimateMigrationComplexity estimates migration difficulty based on tool overlap
func estimateMigrationComplexity(sourceServer, targetServer upstreamv0.ServerJSON) string {
	sourceTools := extractTools(sourceServer)
	targetTools := extractTools(targetServer)

	if len(sourceTools) == 0 && len(targetTools) == 0 {
		return "Low"
	}

	toolScore := scoreToolOverlap(sourceTools, targetTools)

	if toolScore >= 0.8 {
		return "Low" // High tool overlap = easy migration
	} else if toolScore >= 0.5 {
		return "Medium"
	}
	return "High" // Low tool overlap = difficult migration
}

// generateMatchReasons creates human-readable similarity reasons
func generateMatchReasons(sourceServer, targetServer upstreamv0.ServerJSON) []string {
	reasons := []string{}

	// Tag overlap
	sourceTags := extractTags(sourceServer)
	targetTags := extractTags(targetServer)
	commonTags := []string{}
	tagMap := make(map[string]bool)
	for _, tag := range sourceTags {
		tagMap[strings.ToLower(tag)] = true
	}
	for _, tag := range targetTags {
		if tagMap[strings.ToLower(tag)] {
			commonTags = append(commonTags, tag)
		}
	}
	if len(commonTags) > 0 {
		reasons = append(reasons, fmt.Sprintf("shared tags: %s", strings.Join(commonTags, ", ")))
	}

	// Tool overlap
	sourceTools := extractTools(sourceServer)
	targetTools := extractTools(targetServer)
	commonTools := 0
	toolMap := make(map[string]bool)
	for _, tool := range sourceTools {
		toolMap[strings.ToLower(tool)] = true
	}
	for _, tool := range targetTools {
		if toolMap[strings.ToLower(tool)] {
			commonTools++
		}
	}
	if commonTools > 0 {
		reasons = append(reasons, fmt.Sprintf("similar tools: %d/%d", commonTools, len(sourceTools)))
	}

	// Transport match
	if len(sourceServer.Packages) > 0 && len(targetServer.Packages) > 0 {
		sourceTransport := sourceServer.Packages[0].Transport.Type
		targetTransport := targetServer.Packages[0].Transport.Type
		if strings.EqualFold(sourceTransport, targetTransport) {
			reasons = append(reasons, fmt.Sprintf("same transport: %s", sourceTransport))
		}
	}

	// Stars comparison
	sourceStars := extractStars(sourceServer)
	targetStars := extractStars(targetServer)
	if targetStars > sourceStars {
		reasons = append(reasons, fmt.Sprintf("more popular: %d vs %d stars", targetStars, sourceStars))
	}

	return reasons
}

// generateDifferences highlights key differences between servers
func generateDifferences(sourceServer, targetServer upstreamv0.ServerJSON) []string {
	diffs := []string{}

	// Transport difference
	if len(sourceServer.Packages) > 0 && len(targetServer.Packages) > 0 {
		sourceTransport := sourceServer.Packages[0].Transport.Type
		targetTransport := targetServer.Packages[0].Transport.Type
		if !strings.EqualFold(sourceTransport, targetTransport) {
			diffs = append(diffs, fmt.Sprintf("transport: %s vs %s", sourceTransport, targetTransport))
		}
	}

	// Runtime difference
	sourceRuntime := detectRuntime(sourceServer)
	targetRuntime := detectRuntime(targetServer)
	if sourceRuntime != targetRuntime && sourceRuntime != registryTypeUnknown && targetRuntime != registryTypeUnknown {
		diffs = append(diffs, fmt.Sprintf("runtime: %s vs %s", sourceRuntime, targetRuntime))
	}

	// Tier difference
	sourceThMeta := extractToolHiveMetadata(sourceServer)
	targetThMeta := extractToolHiveMetadata(targetServer)
	sourceTier, _ := sourceThMeta["tier"].(string)
	targetTier, _ := targetThMeta["tier"].(string)
	if sourceTier != "" && targetTier != "" && sourceTier != targetTier {
		diffs = append(diffs, fmt.Sprintf("tier: %s vs %s", sourceTier, targetTier))
	}

	return diffs
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

// getSetupGuide implements the get_setup_guide tool
func (s *Server) getSetupGuide(
	ctx context.Context, _ *sdkmcp.CallToolRequest, params *GetSetupGuideParams,
) (*sdkmcp.CallToolResult, any, error) {
	// SDK validates required fields
	serverName := params.ServerName
	platform := params.Platform
	if platform == "" {
		platform = "claude-desktop"
	}
	runtime := params.Runtime

	// Get server from Registry API
	server, err := s.getServerFromAPI(ctx, serverName)
	if err != nil {
		logger.Errorf("Failed to get server %s from API: %v", serverName, err)
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("Error: server not found: %s", serverName)}},
			IsError: true,
		}, nil, nil
	}

	// Auto-detect runtime if not specified
	if runtime == "" {
		runtime = detectRuntime(server)
	}

	// Build setup guide markdown
	var guide strings.Builder

	// Header
	guide.WriteString(fmt.Sprintf("# Setup Guide: %s\n\n", server.Name))
	if server.Description != "" {
		guide.WriteString(fmt.Sprintf("%s\n\n", server.Description))
	}

	// Prerequisites
	guide.WriteString("## Prerequisites\n\n")
	guide.WriteString(fmt.Sprintf("- **Runtime**: %s\n", runtime))
	if len(server.Packages) > 0 {
		guide.WriteString(fmt.Sprintf("- **Transport**: %s\n", server.Packages[0].Transport.Type))
	}
	guide.WriteString("\n")

	// Installation steps
	guide.WriteString("## Installation\n\n")
	guide.WriteString(generateInstallationSteps(server, runtime))

	// Environment variables
	envVars := extractEnvironmentVariables(server)
	if len(envVars) > 0 {
		guide.WriteString("## Environment Variables\n\n")
		guide.WriteString(generateEnvFileExample(envVars))
	}

	// Configuration examples
	guide.WriteString("## Configuration\n\n")
	guide.WriteString(generatePlatformConfig(server, platform))

	// Add other platform examples
	if platform != "cursor" {
		guide.WriteString(generatePlatformConfig(server, "cursor"))
	}
	if platform != "custom" {
		guide.WriteString(generatePlatformConfig(server, "custom"))
	}

	// Troubleshooting
	guide.WriteString(generateTroubleshootingTips(server))

	// Next steps
	guide.WriteString("## Next Steps\n\n")
	if server.Repository != nil && server.Repository.URL != "" {
		guide.WriteString(fmt.Sprintf("- 📚 [Read the documentation](%s#readme)\n", server.Repository.URL))
		guide.WriteString(fmt.Sprintf("- 🐛 [Report issues](%s/issues)\n", server.Repository.URL))
		guide.WriteString(fmt.Sprintf("- ⭐ [Star the project](%s)\n", server.Repository.URL))
	}

	tools := extractTools(server)
	if len(tools) > 0 {
		guide.WriteString(fmt.Sprintf("\n**Available Tools**: %s\n", strings.Join(tools, ", ")))
	}

	// TODO: Add real-time download stats for popularity
	// TODO: Include known issues from GitHub
	// TODO: Add user reviews/ratings when available

	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: guide.String()}},
	}, nil, nil
}

// findAlternatives implements the find_alternatives tool
func (s *Server) findAlternatives(
	ctx context.Context, _ *sdkmcp.CallToolRequest, params *FindAlternativesParams,
) (*sdkmcp.CallToolResult, any, error) {
	// SDK validates required fields
	serverName := params.ServerName
	limit := params.Limit
	if limit == 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}

	// Get the source server
	sourceServer, err := s.getServerFromAPI(ctx, serverName)
	if err != nil {
		logger.Errorf("Failed to get server %s from API: %v", serverName, err)
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("Error: server not found: %s", serverName)}},
			IsError: true,
		}, nil, nil
	}

	// Fetch all servers to compare against
	allServers, err := s.listServersFromAPI(ctx, url.Values{})
	if err != nil {
		logger.Errorf("Failed to fetch servers from API: %v", err)
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("Error: failed to fetch servers: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	// Calculate similarity scores for all servers
	type ScoredAlternative struct {
		Server              upstreamv0.ServerResponse
		SimilarityScore     float64
		MatchReasons        []string
		MigrationComplexity string
		Differences         []string
	}

	alternatives := []ScoredAlternative{}

	for _, serverResp := range allServers.Servers {
		// Skip the source server itself
		if serverResp.Server.Name == sourceServer.Name {
			continue
		}

		score := calculateSimilarityScore(sourceServer, serverResp.Server)

		// Only include servers with meaningful similarity (> 0.1)
		if score > 0.1 {
			alt := ScoredAlternative{
				Server:              serverResp,
				SimilarityScore:     score,
				MatchReasons:        generateMatchReasons(sourceServer, serverResp.Server),
				MigrationComplexity: estimateMigrationComplexity(sourceServer, serverResp.Server),
				Differences:         generateDifferences(sourceServer, serverResp.Server),
			}
			alternatives = append(alternatives, alt)
		}
	}

	// Sort by similarity score descending
	sort.Slice(alternatives, func(i, j int) bool {
		return alternatives[i].SimilarityScore > alternatives[j].SimilarityScore
	})

	// Limit results
	if len(alternatives) > limit {
		alternatives = alternatives[:limit]
	}

	// Build response
	type AlternativeResponse struct {
		Server              upstreamv0.ServerResponse `json:"server"`
		SimilarityScore     float64                   `json:"similarityScore"`
		MatchReasons        []string                  `json:"matchReasons"`
		MigrationComplexity string                    `json:"migrationComplexity"`
		Differences         []string                  `json:"differences,omitempty"`
	}

	response := struct {
		Alternatives []AlternativeResponse `json:"alternatives"`
		Metadata     struct {
			Count           int    `json:"count"`
			SourceServer    string `json:"sourceServer"`
			Reason          string `json:"reason,omitempty"`
			ScoringCriteria string `json:"scoringCriteria"`
		} `json:"metadata"`
	}{
		Alternatives: make([]AlternativeResponse, len(alternatives)),
		Metadata: struct {
			Count           int    `json:"count"`
			SourceServer    string `json:"sourceServer"`
			Reason          string `json:"reason,omitempty"`
			ScoringCriteria string `json:"scoringCriteria"`
		}{
			Count:           len(alternatives),
			SourceServer:    sourceServer.Name,
			Reason:          params.Reason,
			ScoringCriteria: "tags(40%), tools(40%), transport(10%), description(10%)",
		},
	}

	for i, alt := range alternatives {
		response.Alternatives[i] = AlternativeResponse(alt)
	}

	// TODO: Add semantic similarity using embeddings
	// TODO: Include user reviews/ratings when available
	// TODO: Fetch real-time download stats for popularity

	// Return as JSON
	jsonBytes, err := json.MarshalIndent(response, "", "  ")
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

// Journey 2: MCP Developer Tool Handlers

// findSimilarServers implements the find_similar_servers tool
//
//nolint:gocyclo // Journey 2 tool with multiple search paths and scoring logic
func (s *Server) findSimilarServers(
	ctx context.Context, _ *sdkmcp.CallToolRequest, params *FindSimilarServersParams,
) (*sdkmcp.CallToolResult, any, error) {
	limit := params.Limit
	if limit == 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	// Fetch all servers
	allServers, err := s.listServersFromAPI(ctx, url.Values{})
	if err != nil {
		logger.Errorf("Failed to fetch servers from API: %v", err)
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("Error: failed to fetch servers: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	var sourceServer *upstreamv0.ServerJSON

	// Determine search criteria
	if params.ServerName != "" {
		// Find similar to a specific server
		srv, err := s.getServerFromAPI(ctx, params.ServerName)
		if err != nil {
			logger.Errorf("Failed to get server %s from API: %v", params.ServerName, err)
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("Error: server not found: %s", params.ServerName)}},
				IsError: true,
			}, nil, nil
		}
		sourceServer = &srv
	}

	// Calculate similarity scores
	type ScoredServer struct {
		Server          upstreamv0.ServerResponse
		SimilarityScore float64
		MatchReasons    []string
	}

	similar := []ScoredServer{}

	for _, serverResp := range allServers.Servers {
		var score float64
		var reasons []string

		if sourceServer != nil {
			// Similarity to reference server
			if serverResp.Server.Name == sourceServer.Name {
				continue // Skip the source server itself
			}
			score = calculateSimilarityScore(*sourceServer, serverResp.Server)
			reasons = generateMatchReasons(*sourceServer, serverResp.Server)
		} else {
			// Match based on provided tags/tools
			tagScore := 0.0
			toolScore := 0.0

			if len(params.Tags) > 0 {
				serverTags := extractTags(serverResp.Server)
				tagScore = scoreTagOverlap(params.Tags, serverTags)
			}

			if len(params.Tools) > 0 {
				serverTools := extractTools(serverResp.Server)
				toolScore = scoreToolOverlap(params.Tools, serverTools)
			}

			// Weight: tags 50%, tools 50% when searching by criteria
			if len(params.Tags) > 0 && len(params.Tools) > 0 {
				score = (tagScore * 0.5) + (toolScore * 0.5)
			} else if len(params.Tags) > 0 {
				score = tagScore
			} else if len(params.Tools) > 0 {
				score = toolScore
			} else {
				continue // No criteria provided
			}

			// Generate reasons
			if tagScore > 0 {
				matchedTags := []string{}
				serverTags := extractTags(serverResp.Server)
				tagMap := make(map[string]bool)
				for _, tag := range serverTags {
					tagMap[strings.ToLower(tag)] = true
				}
				for _, tag := range params.Tags {
					if tagMap[strings.ToLower(tag)] {
						matchedTags = append(matchedTags, tag)
					}
				}
				if len(matchedTags) > 0 {
					reasons = append(reasons, fmt.Sprintf("tags: %s", strings.Join(matchedTags, ", ")))
				}
			}

			if toolScore > 0 {
				matchedTools := 0
				serverTools := extractTools(serverResp.Server)
				toolMap := make(map[string]bool)
				for _, tool := range serverTools {
					toolMap[strings.ToLower(tool)] = true
				}
				for _, tool := range params.Tools {
					if toolMap[strings.ToLower(tool)] {
						matchedTools++
					}
				}
				if matchedTools > 0 {
					reasons = append(reasons, fmt.Sprintf("tools: %d/%d", matchedTools, len(params.Tools)))
				}
			}
		}

		// Only include servers with meaningful similarity (> 0.1)
		if score > 0.1 {
			similar = append(similar, ScoredServer{
				Server:          serverResp,
				SimilarityScore: score,
				MatchReasons:    reasons,
			})
		}
	}

	// Sort by similarity score descending
	sort.Slice(similar, func(i, j int) bool {
		return similar[i].SimilarityScore > similar[j].SimilarityScore
	})

	// Limit results
	if len(similar) > limit {
		similar = similar[:limit]
	}

	// Build response
	type SimilarServerResponse struct {
		Server          upstreamv0.ServerResponse `json:"server"`
		SimilarityScore float64                   `json:"similarityScore"`
		MatchReasons    []string                  `json:"matchReasons"`
	}

	response := struct {
		Servers  []SimilarServerResponse `json:"servers"`
		Metadata struct {
			Count          int    `json:"count"`
			SearchCriteria string `json:"searchCriteria"`
		} `json:"metadata"`
	}{
		Servers: make([]SimilarServerResponse, len(similar)),
	}

	for i, sim := range similar {
		response.Servers[i] = SimilarServerResponse(sim)
	}

	response.Metadata.Count = len(similar)
	if sourceServer != nil {
		response.Metadata.SearchCriteria = fmt.Sprintf("similar to %s", sourceServer.Name)
	} else if len(params.Tags) > 0 && len(params.Tools) > 0 {
		response.Metadata.SearchCriteria = fmt.Sprintf("tags: %s, tools: %s",
			strings.Join(params.Tags, ", "), strings.Join(params.Tools, ", "))
	} else if len(params.Tags) > 0 {
		response.Metadata.SearchCriteria = fmt.Sprintf("tags: %s", strings.Join(params.Tags, ", "))
	} else if len(params.Tools) > 0 {
		response.Metadata.SearchCriteria = fmt.Sprintf("tools: %s", strings.Join(params.Tools, ", "))
	}

	// Return as JSON
	jsonBytes, err := json.MarshalIndent(response, "", "  ")
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

// getServerAnalytics implements the get_server_analytics tool
func (s *Server) getServerAnalytics(
	ctx context.Context, _ *sdkmcp.CallToolRequest, params *GetServerAnalyticsParams,
) (*sdkmcp.CallToolResult, any, error) {
	serverName := params.ServerName
	period := params.Period
	if period == "" {
		period = "30d"
	}

	// Get server from Registry API
	server, err := s.getServerFromAPI(ctx, serverName)
	if err != nil {
		logger.Errorf("Failed to get server %s from API: %v", serverName, err)
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("Error: server not found: %s", serverName)}},
			IsError: true,
		}, nil, nil
	}

	// Extract current metrics
	stars := extractStars(server)
	pulls := extractPulls(server)
	tools := extractTools(server)
	tags := extractTags(server)

	// Build analytics response with derived data
	response := struct {
		ServerName string `json:"serverName"`
		Period     string `json:"period"`
		Current    struct {
			Stars int64    `json:"stars"`
			Pulls int64    `json:"pulls"`
			Tools int      `json:"toolCount"`
			Tags  []string `json:"tags"`
		} `json:"current"`
		Trends struct {
			Message string `json:"message"`
			// TODO: Add real-time series data when available
			StarsGrowth string `json:"starsGrowth,omitempty"`
			PullsGrowth string `json:"pullsGrowth,omitempty"`
		} `json:"trends"`
		Popularity struct {
			Rank       string `json:"rank"`
			Percentile string `json:"percentile"`
			ComparedTo string `json:"comparedTo"`
		} `json:"popularity"`
		Recommendations []string `json:"recommendations"`
	}{
		ServerName: serverName,
		Period:     period,
	}

	response.Current.Stars = stars
	response.Current.Pulls = pulls
	response.Current.Tools = len(tools)
	response.Current.Tags = tags

	// Derive popularity rank (placeholder logic based on stars)
	if stars > 1000 {
		response.Popularity.Rank = "Top Tier"
		response.Popularity.Percentile = "Top 5%"
	} else if stars > 500 {
		response.Popularity.Rank = "High"
		response.Popularity.Percentile = "Top 15%"
	} else if stars > 100 {
		response.Popularity.Rank = "Medium"
		response.Popularity.Percentile = "Top 40%"
	} else {
		response.Popularity.Rank = "Growing"
		response.Popularity.Percentile = "Emerging"
	}
	response.Popularity.ComparedTo = "all registered MCP servers"

	// TODO: Calculate real trends when time-series data is available
	response.Trends.Message = "Historical trend data not yet available. Showing current snapshot."

	// Generate recommendations
	if stars < 50 {
		response.Recommendations = append(response.Recommendations, "Consider promoting your server on GitHub and social media")
	}
	if len(tools) < 3 {
		response.Recommendations = append(response.Recommendations, "Adding more tools could increase adoption")
	}
	if len(tags) < 3 {
		response.Recommendations = append(response.Recommendations, "Add more descriptive tags to improve discoverability")
	}
	if len(server.Packages) == 0 {
		response.Recommendations = append(response.Recommendations, "Add package information to make installation easier")
	}

	// Return as JSON
	jsonBytes, err := json.MarshalIndent(response, "", "  ")
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

// getEcosystemInsights implements the get_ecosystem_insights tool
//
//nolint:gocyclo // Journey 2 tool with ecosystem analysis and aggregation logic
func (s *Server) getEcosystemInsights(
	ctx context.Context, _ *sdkmcp.CallToolRequest, params *GetEcosystemInsightsParams,
) (*sdkmcp.CallToolResult, any, error) {
	category := params.Category
	if category == "" {
		category = "all"
	}

	// Fetch all servers
	allServers, err := s.listServersFromAPI(ctx, url.Values{})
	if err != nil {
		logger.Errorf("Failed to fetch servers from API: %v", err)
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf("Error: failed to fetch servers: %v", err)}},
			IsError: true,
		}, nil, nil
	}

	// Analyze ecosystem
	tagFrequency := make(map[string]int)
	toolFrequency := make(map[string]int)
	transportFrequency := make(map[string]int)
	runtimeFrequency := make(map[string]int)
	totalStars := int64(0)
	totalPulls := int64(0)

	filteredServers := []upstreamv0.ServerResponse{}

	for _, serverResp := range allServers.Servers {
		// Filter by category if specified
		if category != "all" {
			tags := extractTags(serverResp.Server)
			matchesCategory := false
			categoryLower := strings.ToLower(category)
			for _, tag := range tags {
				if strings.Contains(strings.ToLower(tag), categoryLower) {
					matchesCategory = true
					break
				}
			}
			if !matchesCategory {
				continue
			}
		}

		filteredServers = append(filteredServers, serverResp)

		// Collect statistics
		tags := extractTags(serverResp.Server)
		for _, tag := range tags {
			tagFrequency[tag]++
		}

		tools := extractTools(serverResp.Server)
		for _, tool := range tools {
			toolFrequency[tool]++
		}

		if len(serverResp.Server.Packages) > 0 {
			pkg := serverResp.Server.Packages[0]
			transportFrequency[pkg.Transport.Type]++
			runtime := detectRuntime(serverResp.Server)
			if runtime != registryTypeUnknown {
				runtimeFrequency[runtime]++
			}
		}

		totalStars += extractStars(serverResp.Server)
		totalPulls += extractPulls(serverResp.Server)
	}

	// Find top items
	topTags := getTopN(tagFrequency, 10)
	topTools := getTopN(toolFrequency, 10)
	topTransports := getTopN(transportFrequency, 5)
	topRuntimes := getTopN(runtimeFrequency, 5)

	// Build response
	response := struct {
		Category string `json:"category"`
		Overview struct {
			TotalServers int   `json:"totalServers"`
			TotalStars   int64 `json:"totalStars"`
			TotalPulls   int64 `json:"totalPulls"`
			AvgStars     int64 `json:"avgStars"`
			AvgPulls     int64 `json:"avgPulls"`
		} `json:"overview"`
		TopTags       []FreqItem `json:"topTags"`
		TopTools      []FreqItem `json:"topTools"`
		Transports    []FreqItem `json:"transports"`
		Runtimes      []FreqItem `json:"runtimes"`
		Insights      []string   `json:"insights"`
		Opportunities []string   `json:"opportunities"`
	}{
		Category:   category,
		TopTags:    topTags,
		TopTools:   topTools,
		Transports: topTransports,
		Runtimes:   topRuntimes,
	}

	response.Overview.TotalServers = len(filteredServers)
	response.Overview.TotalStars = totalStars
	response.Overview.TotalPulls = totalPulls
	if len(filteredServers) > 0 {
		response.Overview.AvgStars = totalStars / int64(len(filteredServers))
		response.Overview.AvgPulls = totalPulls / int64(len(filteredServers))
	}

	// Generate insights
	if len(topTransports) > 0 {
		response.Insights = append(response.Insights,
			fmt.Sprintf("Most popular transport: %s (%d servers)", topTransports[0].Name, topTransports[0].Count))
	}
	if len(topRuntimes) > 0 {
		response.Insights = append(response.Insights,
			fmt.Sprintf("Most common runtime: %s (%d servers)", topRuntimes[0].Name, topRuntimes[0].Count))
	}
	if len(topTags) > 0 {
		response.Insights = append(response.Insights,
			fmt.Sprintf("Most popular category: %s (%d servers)", topTags[0].Name, topTags[0].Count))
	}

	// Identify opportunities (underserved areas)
	response.Opportunities = append(response.Opportunities,
		"Areas with fewer than 5 servers represent opportunities for new implementations")

	// TODO: Add real trend analysis when time-series data is available
	response.Opportunities = append(response.Opportunities,
		"Growth trends and emerging categories will be available with historical data")

	// Return as JSON
	jsonBytes, err := json.MarshalIndent(response, "", "  ")
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

// getTopN returns top N items from a frequency map
func getTopN(freq map[string]int, n int) []FreqItem {
	items := make([]FreqItem, 0, len(freq))
	for name, count := range freq {
		items = append(items, FreqItem{Name: name, Count: count})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Count > items[j].Count
	})

	if len(items) > n {
		items = items[:n]
	}

	return items
}

// analyzeToolOverlap implements the analyze_tool_overlap tool
//
//nolint:gocyclo // Journey 2 tool with overlap matrix calculation and unique tool detection
func (s *Server) analyzeToolOverlap(
	ctx context.Context, _ *sdkmcp.CallToolRequest, params *AnalyzeToolOverlapParams,
) (*sdkmcp.CallToolResult, any, error) {
	serverNames := params.ServerNames

	if len(serverNames) < 2 || len(serverNames) > 10 {
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "Error: provide between 2 and 10 servers to analyze"}},
			IsError: true,
		}, nil, nil
	}

	// Fetch all servers
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

	// Extract tools for each server
	serverTools := make(map[string][]string)
	allTools := make(map[string]bool)

	for _, server := range servers {
		tools := extractTools(server)
		serverTools[server.Name] = tools
		for _, tool := range tools {
			allTools[tool] = true
		}
	}

	// Calculate overlap matrix
	type OverlapEntry struct {
		ServerA string  `json:"serverA"`
		ServerB string  `json:"serverB"`
		Overlap float64 `json:"overlapScore"`
		Shared  int     `json:"sharedTools"`
	}

	overlapMatrix := []OverlapEntry{}
	for i, serverA := range servers {
		for j, serverB := range servers {
			if i < j {
				toolsA := serverTools[serverA.Name]
				toolsB := serverTools[serverB.Name]
				score := scoreToolOverlap(toolsA, toolsB)

				// Count shared tools
				shared := 0
				toolMapA := make(map[string]bool)
				for _, tool := range toolsA {
					toolMapA[strings.ToLower(tool)] = true
				}
				for _, tool := range toolsB {
					if toolMapA[strings.ToLower(tool)] {
						shared++
					}
				}

				overlapMatrix = append(overlapMatrix, OverlapEntry{
					ServerA: serverA.Name,
					ServerB: serverB.Name,
					Overlap: score,
					Shared:  shared,
				})
			}
		}
	}

	// Sort by overlap score descending
	sort.Slice(overlapMatrix, func(i, j int) bool {
		return overlapMatrix[i].Overlap > overlapMatrix[j].Overlap
	})

	// Find unique tools per server if requested
	type ServerToolInfo struct {
		ServerName  string   `json:"serverName"`
		TotalTools  int      `json:"totalTools"`
		UniqueTools []string `json:"uniqueTools,omitempty"`
	}

	serverInfo := make([]ServerToolInfo, len(servers))
	for i, server := range servers {
		tools := serverTools[server.Name]
		info := ServerToolInfo{
			ServerName: server.Name,
			TotalTools: len(tools),
		}

		if params.ShowUnique {
			unique := []string{}
			for _, tool := range tools {
				// Check if tool is unique to this server
				isUnique := true
				for _, otherServer := range servers {
					if otherServer.Name == server.Name {
						continue
					}
					otherTools := serverTools[otherServer.Name]
					toolMap := make(map[string]bool)
					for _, t := range otherTools {
						toolMap[strings.ToLower(t)] = true
					}
					if toolMap[strings.ToLower(tool)] {
						isUnique = false
						break
					}
				}
				if isUnique {
					unique = append(unique, tool)
				}
			}
			info.UniqueTools = unique
		}

		serverInfo[i] = info
	}

	// Build response
	response := struct {
		Servers       []ServerToolInfo `json:"servers"`
		OverlapMatrix []OverlapEntry   `json:"overlapMatrix"`
		Summary       struct {
			TotalServers     int     `json:"totalServers"`
			TotalUniqueTools int     `json:"totalUniqueTools"`
			AvgOverlap       float64 `json:"avgOverlap"`
		} `json:"summary"`
		Insights []string `json:"insights"`
	}{
		Servers:       serverInfo,
		OverlapMatrix: overlapMatrix,
	}

	response.Summary.TotalServers = len(servers)
	response.Summary.TotalUniqueTools = len(allTools)

	if len(overlapMatrix) > 0 {
		sum := 0.0
		for _, entry := range overlapMatrix {
			sum += entry.Overlap
		}
		response.Summary.AvgOverlap = sum / float64(len(overlapMatrix))
	}

	// Generate insights
	if response.Summary.AvgOverlap > 0.7 {
		response.Insights = append(response.Insights, "High overlap detected - servers are competing for similar use cases")
	} else if response.Summary.AvgOverlap < 0.3 {
		response.Insights = append(response.Insights, "Low overlap detected - servers are complementary and serve different needs")
	} else {
		response.Insights = append(response.Insights, "Moderate overlap - some shared functionality with unique features")
	}

	if len(overlapMatrix) > 0 {
		highest := overlapMatrix[0]
		response.Insights = append(response.Insights,
			fmt.Sprintf("Highest overlap: %s ↔ %s (%.1f%% similar, %d shared tools)",
				highest.ServerA, highest.ServerB, highest.Overlap*100, highest.Shared))
	}

	// Return as JSON
	jsonBytes, err := json.MarshalIndent(response, "", "  ")
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
