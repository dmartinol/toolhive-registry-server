#!/bin/bash
# ToolHive Registry API with MCP Integration - Usage Example
# This demonstrates using the MCP endpoints when launched with --enable-mcp

set -e

API_URL="${API_URL:-http://localhost:8080}"
MCP_URL="$API_URL/mcp"

echo "============================================"
echo "ToolHive Registry API + MCP Integration"
echo "API URL: $API_URL"
echo "MCP URL: $MCP_URL"
echo "============================================"
echo

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to make MCP requests
mcp_request() {
    local method="$1"
    local params="$2"
    local id="${3:-1}"
    
    if [ -z "$params" ]; then
        params="{}"
    fi
    
    echo -e "${BLUE}Request:${NC} $method"
    
    curl -s -X POST "$MCP_URL" \
        -H "Content-Type: application/json" \
        -d "{
            \"jsonrpc\": \"2.0\",
            \"id\": $id,
            \"method\": \"$method\",
            \"params\": $params
        }" | jq '.'
}

# Function to make REST API requests
rest_request() {
    local endpoint="$1"
    echo -e "${BLUE}REST API:${NC} GET $endpoint"
    curl -s "$API_URL$endpoint" | jq '.'
}

echo -e "${GREEN}=== STEP 1: Check Server Health ===${NC}"
echo "First, let's verify both the API and MCP endpoints are running..."
echo

echo -e "${YELLOW}Checking API health:${NC}"
rest_request "/health"
echo

echo -e "${YELLOW}Checking API version:${NC}"
rest_request "/version"
echo

echo "Press Enter to continue..."
read -r

echo -e "${GREEN}=== STEP 2: Initialize MCP Connection ===${NC}"
echo "MCP clients must initialize before using tools..."
echo

mcp_request "initialize" '{
    "protocolVersion": "2024-11-05",
    "capabilities": {},
    "clientInfo": {
        "name": "example-client",
        "version": "1.0.0"
    }
}' 1
echo

echo "Press Enter to continue..."
read -r

echo -e "${GREEN}=== STEP 3: Discover Available MCP Tools ===${NC}"
echo "List all tools provided by the MCP server..."
echo

mcp_request "tools/list" "{}" 2
echo

echo "Press Enter to continue..."
read -r

echo -e "${GREEN}=== STEP 4: Natural Language Search ===${NC}"
echo "Example: 'What database tools are available?'"
echo

mcp_request "tools/call" '{
    "name": "search_servers",
    "arguments": {
        "query": "database",
        "limit": 3
    }
}' 3
echo

echo "Press Enter to continue..."
read -r

echo -e "${GREEN}=== STEP 5: Get Server Details ===${NC}"
echo "Get detailed information about a specific server..."
echo

mcp_request "tools/call" '{
    "name": "get_server_details",
    "arguments": {
        "server_name": "io.github.stacklok/everything"
    }
}' 4
echo

echo "Press Enter to continue..."
read -r

echo -e "${GREEN}=== STEP 6: List Top Servers by Stars ===${NC}"
echo "Find the most popular servers..."
echo

mcp_request "tools/call" '{
    "name": "list_servers",
    "arguments": {
        "limit": 5,
        "sort_by": "stars"
    }
}' 5
echo

echo "Press Enter to continue..."
read -r

echo -e "${GREEN}=== STEP 7: Compare Multiple Servers ===${NC}"
echo "Compare servers side-by-side..."
echo

mcp_request "tools/call" '{
    "name": "compare_servers",
    "arguments": {
        "server_names": [
            "io.github.stacklok/everything",
            "io.github.stacklok/filesystem"
        ]
    }
}' 6
echo

echo "Press Enter to continue..."
read -r

echo -e "${GREEN}=== STEP 8: Advanced Search with Tags ===${NC}"
echo "Search with tag filtering..."
echo

mcp_request "tools/call" '{
    "name": "search_servers",
    "arguments": {
        "query": "tool",
        "tags": ["files"],
        "limit": 3
    }
}' 7
echo

echo "Press Enter to continue..."
read -r

echo -e "${GREEN}=== STEP 9: Compare REST API vs MCP ===${NC}"
echo "The same registry data is available through both interfaces!"
echo

echo -e "${YELLOW}Using REST API:${NC}"
rest_request "/registry/v0.1/servers?limit=2"
echo

echo -e "${YELLOW}Using MCP Protocol:${NC}"
mcp_request "tools/call" '{
    "name": "list_servers",
    "arguments": {
        "limit": 2,
        "sort_by": "name"
    }
}' 8
echo

echo -e "${GREEN}=== STEP 10: SSE Endpoint Test ===${NC}"
echo "The MCP server also supports Server-Sent Events for streaming..."
echo "Testing SSE endpoint availability:"
echo

curl -s -N -H "Accept: text/event-stream" "$MCP_URL/sse" &
SSE_PID=$!
sleep 2
kill $SSE_PID 2>/dev/null || true
echo "SSE endpoint is responsive!"
echo

echo "============================================"
echo -e "${GREEN}Examples completed!${NC}"
echo "============================================"
echo
echo "Summary:"
echo "• MCP endpoints are available at: $MCP_URL"
echo "• REST API endpoints are available at: $API_URL"
echo "• Both share the same registry data"
echo "• MCP provides natural language interface for AI assistants"
echo "• REST API provides programmatic access for applications"
echo
echo "Next steps:"
echo "1. Integrate with Claude Desktop (see examples/claude-desktop-config.json)"
echo "2. Use MCP for AI-powered server discovery"
echo "3. Use REST API for programmatic access"
echo "4. See MCPServer.md for complete documentation"

