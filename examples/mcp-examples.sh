#!/bin/bash
# ToolHive Registry MCP Server - Usage Examples
# This script demonstrates how to interact with the MCP server

set -e

MCP_URL="${MCP_URL:-http://localhost:8081}"

echo "============================================"
echo "ToolHive Registry MCP Server Examples"
echo "Server: $MCP_URL"
echo "============================================"
echo

# Function to make JSON-RPC requests
make_request() {
    local method="$1"
    local params="$2"
    local id="${3:-1}"
    
    if [ -z "$params" ]; then
        params="{}"
    fi
    
    curl -s -X POST "$MCP_URL" \
        -H "Content-Type: application/json" \
        -d "{
            \"jsonrpc\": \"2.0\",
            \"id\": $id,
            \"method\": \"$method\",
            \"params\": $params
        }" | jq '.'
}

# Example 1: Initialize connection
echo "1. Initialize MCP connection"
echo "----------------------------"
make_request "initialize" '{
    "protocolVersion": "2024-11-05",
    "capabilities": {},
    "clientInfo": {
        "name": "example-client",
        "version": "1.0.0"
    }
}'
echo
echo "Press Enter to continue..."
read -r

# Example 2: List available tools
echo "2. List available MCP tools"
echo "---------------------------"
make_request "tools/list" "{}" 2
echo
echo "Press Enter to continue..."
read -r

# Example 3: Search for database servers
echo "3. Search for database servers"
echo "-------------------------------"
make_request "tools/call" '{
    "name": "search_servers",
    "arguments": {
        "query": "database",
        "limit": 3
    }
}' 3
echo
echo "Press Enter to continue..."
read -r

# Example 4: Get server details
echo "4. Get server details"
echo "---------------------"
make_request "tools/call" '{
    "name": "get_server_details",
    "arguments": {
        "server_name": "io.github.stacklok/everything"
    }
}' 4
echo
echo "Press Enter to continue..."
read -r

# Example 5: List servers sorted by stars
echo "5. List servers sorted by stars"
echo "--------------------------------"
make_request "tools/call" '{
    "name": "list_servers",
    "arguments": {
        "limit": 5,
        "sort_by": "stars"
    }
}' 5
echo
echo "Press Enter to continue..."
read -r

# Example 6: Compare servers
echo "6. Compare multiple servers"
echo "---------------------------"
make_request "tools/call" '{
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

# Example 7: Search with tags
echo "7. Search with tag filtering"
echo "----------------------------"
make_request "tools/call" '{
    "name": "search_servers",
    "arguments": {
        "query": "server",
        "tags": ["database", "sql"],
        "limit": 3
    }
}' 7
echo

echo "============================================"
echo "Examples completed!"
echo "============================================"

