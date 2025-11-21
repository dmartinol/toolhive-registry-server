#!/usr/bin/env bash
# Example: Connect MCP Server to Official MCP Registry
# This demonstrates how to run thv-registry-mcp against the official registry

set -e

echo "Starting MCP server connected to official MCP Registry..."
echo "Registry URL: https://registry.modelcontextprotocol.io"
echo ""

# Run in stdio mode (for Cursor/Claude Desktop integration)
./bin/thv-registry-mcp serve \
  --registry-url https://registry.modelcontextprotocol.io \
  --transport stdio

# To run in HTTP mode instead (for testing):
# ./bin/thv-registry-mcp serve \
#   --registry-url https://registry.modelcontextprotocol.io \
#   --transport http \
#   --address :8082

