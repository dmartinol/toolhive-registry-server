# Quick Start: Integrated MCP + REST API

This guide shows how to run the ToolHive Registry API with MCP endpoints enabled.

## Prerequisites

- Built binary: `bin/thv-registry-api`
- Configuration file (any from `examples/` directory)
- Registry data in `data/registry.json`

## Step 1: Start the Server

Start the API server with MCP endpoints enabled:

```bash
cd /path/to/toolhive-registry-server

# Start with MCP enabled
./bin/thv-registry-api serve \
  --config examples/config-file.yaml \
  --address :8080 \
  --enable-mcp
```

You should see logs indicating:
```
MCP endpoints enabled at /mcp
Starting registry API server on :8080
```

## Step 2: Verify It's Running

Check both API and MCP endpoints:

```bash
# Check API health
curl http://localhost:8080/health

# Check API version
curl http://localhost:8080/version

# Initialize MCP connection
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2024-11-05",
      "capabilities": {},
      "clientInfo": {"name": "test", "version": "1.0"}
    }
  }'
```

## Step 3: Try the MCP Tools

### List Available Tools

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list"
  }' | jq '.result.tools[] | {name, description}'
```

Expected output:
```json
{
  "name": "search_servers",
  "description": "Search for MCP servers by keywords, tags, or use case..."
}
{
  "name": "get_server_details",
  "description": "Get comprehensive information about a specific MCP server..."
}
{
  "name": "list_servers",
  "description": "List all available MCP servers with filtering and pagination..."
}
{
  "name": "compare_servers",
  "description": "Compare multiple MCP servers side-by-side..."
}
```

### Search for Servers

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "search_servers",
      "arguments": {
        "query": "database tool with most stars",
        "limit": 3
      }
    }
  }' | jq '.result.content[0].text'
```

### Get Server Details

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
      "name": "get_server_details",
      "arguments": {
        "server_name": "io.github.stacklok/everything"
      }
    }
  }' | jq '.result.content[0].text'
```

### List Top Servers

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 4,
    "method": "tools/call",
    "params": {
      "name": "list_servers",
      "arguments": {
        "limit": 5,
        "sort_by": "stars"
      }
    }
  }' | jq '.result.content[0].text'
```

### Compare Servers

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 5,
    "method": "tools/call",
    "params": {
      "name": "compare_servers",
      "arguments": {
        "server_names": [
          "io.github.stacklok/everything",
          "io.github.stacklok/filesystem"
        ]
      }
    }
  }' | jq '.result.content[0].text'
```

## Step 4: Run the Interactive Demo

For an interactive walkthrough:

```bash
./examples/mcp-integrated-example.sh
```

This script demonstrates:
- Health checks
- MCP initialization
- All 4 MCP tools
- REST API comparison
- SSE endpoint

## Both APIs in One Server!

The integrated mode gives you both interfaces:

### REST API Endpoints

All standard REST endpoints remain available:

```bash
# Get all servers
curl http://localhost:8080/registry/v0.1/servers

# Get specific server
curl http://localhost:8080/registry/v0.1/servers/io.github.stacklok%2Feverything/versions/latest

# Get all versions
curl http://localhost:8080/registry/v0.1/servers/io.github.stacklok%2Feverything/versions
```

### MCP Endpoints

MCP protocol for AI assistants:

```bash
# Via POST /mcp
curl -X POST http://localhost:8080/mcp -d '...'

# Via SSE /mcp/sse
curl -N -H "Accept: text/event-stream" http://localhost:8080/mcp/sse

# Via explicit /mcp/jsonrpc
curl -X POST http://localhost:8080/mcp/jsonrpc -d '...'
```

## Use Cases

### Use REST API when:
- Building web applications
- Integrating with CI/CD pipelines
- Programmatic server discovery
- Batch processing

### Use MCP when:
- Integrating with AI assistants (Claude, ChatGPT, etc.)
- Natural language queries
- Interactive exploration
- AI-powered recommendations

## Configuration Options

### Enable MCP via CLI

```bash
thv-registry-api serve \
  --config config.yaml \
  --enable-mcp              # Enable MCP endpoints
  --address :8080           # Port for both APIs
```

### Environment Variables

```bash
# Override config file location
export CONFIG=/path/to/config.yaml

# Start server
thv-registry-api serve --enable-mcp
```

## Deployment

### Docker

```dockerfile
FROM golang:1.23-alpine
COPY bin/thv-registry-api /usr/local/bin/
COPY examples/config-file.yaml /etc/thv-registry/config.yaml
COPY data/registry.json /var/lib/thv-registry/registry.json

EXPOSE 8080

CMD ["thv-registry-api", "serve", \
     "--config", "/etc/thv-registry/config.yaml", \
     "--address", ":8080", \
     "--enable-mcp"]
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: thv-registry-api
spec:
  replicas: 1
  selector:
    matchLabels:
      app: thv-registry-api
  template:
    metadata:
      labels:
        app: thv-registry-api
    spec:
      containers:
      - name: api
        image: ghcr.io/stacklok/toolhive-registry-server:latest
        args:
          - serve
          - --config=/etc/config/config.yaml
          - --address=:8080
          - --enable-mcp
        ports:
        - containerPort: 8080
          name: http
        volumeMounts:
        - name: config
          mountPath: /etc/config
      volumes:
      - name: config
        configMap:
          name: thv-registry-config
---
apiVersion: v1
kind: Service
metadata:
  name: thv-registry-api
spec:
  selector:
    app: thv-registry-api
  ports:
  - port: 8080
    targetPort: 8080
    name: http
```

## Monitoring

### Health Checks

```bash
# Basic health
curl http://localhost:8080/health

# Readiness (checks registry loaded)
curl http://localhost:8080/readiness

# MCP initialization (confirms MCP is working)
curl -X POST http://localhost:8080/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"healthcheck","version":"1.0"}}}'
```

### Logs

Look for these messages:

```
INFO  MCP endpoints enabled at /mcp
INFO  Starting registry API server on :8080
INFO  HTTP server configured on :8080
```

## Troubleshooting

### MCP endpoints return 404

**Problem**: `curl http://localhost:8080/mcp` returns 404

**Solution**: Ensure `--enable-mcp` flag is used:
```bash
thv-registry-api serve --config config.yaml --enable-mcp
```

### Tools return "server not found"

**Problem**: MCP tools can't find servers

**Solution**: Verify registry data is loaded:
```bash
curl http://localhost:8080/registry/v0.1/servers
```

### JSON-RPC errors

**Problem**: Getting JSON-RPC protocol errors

**Solution**: Ensure request format is correct:
```json
{
  "jsonrpc": "2.0",        // Required
  "id": 1,                 // Required
  "method": "tools/list",  // Required
  "params": {}             // Optional
}
```

## Next Steps

1. **Integrate with Claude Desktop**: See `examples/claude-desktop-config.json`
2. **Read full documentation**: See `MCPServer.md`
3. **Explore standalone mode**: Try `thv-registry-mcp` for stdio transport
4. **Build applications**: Use REST API for programmatic access

## Support

- Issues: https://github.com/stacklok/toolhive-registry-server/issues
- Docs: See `MCPServer.md` and `README.md`
- Examples: See `examples/` directory

