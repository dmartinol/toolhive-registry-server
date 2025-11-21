# ToolHive Registry MCP Server

## Overview

The ToolHive Registry MCP Server (`thv-registry-mcp`) provides an MCP (Model Context Protocol) interface to the ToolHive Registry, enabling AI assistants to discover and query MCP servers using natural language.

**Architecture**: The MCP server acts as a lightweight MCP-to-REST bridge that connects to an existing ToolHive Registry API server. It does not manage registry data directly but proxies requests to the registry API, translating between MCP tools and REST endpoints.

## Features

- **Official Go SDK**: Built on the [official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) for guaranteed spec compliance
- **Standards Compliant**: Automatically tracks MCP specification updates via SDK
- **Type-Safe Tools**: Automatic JSON schema generation from Go structs
- **Natural Language Search**: Search for servers using natural language queries
- **Rich Metadata**: Access ToolHive-specific metadata including stars, pulls, tools, and more
- **Server Comparison**: Compare multiple servers side-by-side
- **Two Deployment Modes**: Run as standalone server or integrated with API server
- **Multiple Transports**: Supports HTTP (StreamableHTTP) and stdio transport modes

## Architecture

### Components

```
┌─────────────────────────────────────────────────────────────┐
│                    AI Assistant (Claude, etc.)              │
└────────────────────────┬────────────────────────────────────┘
                         │ MCP Protocol (stdio/HTTP)
                         ↓
┌─────────────────────────────────────────────────────────────┐
│              thv-registry-mcp (MCP Server)                  │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Official Go MCP SDK v1.1.0                          │   │
│  │  • JSON-RPC protocol handling                        │   │
│  │  • Transport management (stdio/HTTP)                 │   │
│  │  • Automatic schema generation                       │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  MCP Tools (9 tools, ~2500 lines)                    │   │
│  │  Journey 1 (Consumer):                               │   │
│  │  • search_servers, get_server_details,              │   │
│  │    compare_servers, get_setup_guide,                │   │
│  │    find_alternatives                                  │   │
│  │  Journey 2 (Developer):                              │   │
│  │  • find_similar_servers, get_server_analytics,      │   │
│  │    get_ecosystem_insights, analyze_tool_overlap     │   │
│  └──────────────────────────────────────────────────────┘   │
└────────────────────────┬────────────────────────────────────┘
                         │ REST API (HTTP)
                         ↓
┌─────────────────────────────────────────────────────────────┐
│         thv-registry-api (Registry API Server)              │
│         • Manages registry data sources                     │
│         • Git/API/File synchronization                      │
│         • Background sync coordinator                       │
└─────────────────────────────────────────────────────────────┘
```

**Source Structure**:
```
cmd/thv-registry-mcp/       # Standalone MCP server command
├── main.go                 # Entry point
└── app/
    ├── serve.go           # Serve command with stdio/HTTP modes
    └── version.go         # Version command

internal/mcp/              # MCP implementation (SDK-based)
├── server.go              # SDK server wrapper & tool registration (~70 lines)
└── tools.go               # Tool implementations with typed parameters (~400 lines)
```

**Implementation**: 
- Uses the official [Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk) v1.1.0 for all MCP protocol handling
- Acts as a stateless bridge between MCP clients and the Registry API
- No local data storage or synchronization - all data fetched from Registry API
- Translates MCP tool calls into REST API requests

### MCP Tools

The server exposes 9 powerful tools for AI assistants (5 for Journey 1, 4 for Journey 2):

#### 1. `search_servers`

Search for MCP servers with automatic pagination support for agents. Returns a single page by default (fast), with cursor-based iteration for complete results.

**All Parameters (All Optional):**

**Search & Filter:**
- `query` (string): Natural language query or keywords (searches name, description, tools)
- `name` (string): Filter by server name (substring match)
- `tags` (array): Filter by tags (must have all specified tags)
- `tools` (array): Filter by tool names (must have all specified tools)
- `transport` (string): Filter by transport type (e.g., "stdio", "http", "sse")

**Metadata Filters:**
- `min_stars` (number): Minimum star count
- `min_pulls` (number): Minimum pull count
- `tier` (string): Filter by tier
- `status` (string): Filter by status

**Pagination Control:**
- `cursor` (string): Pagination cursor from previous response (for iterating through results)
- `limit` (number): Max results per call (default: 20, max: 1000)

**Other:**
- `version_filter` (string): Filter by version
- `sort_by` (string): Sort by "stars", "pulls", "name", or "updated_at"

**Agent Iteration Pattern:**

The tool supports multi-call iteration for large result sets:

1. **First call** (no cursor):
```json
{
  "query": "database",
  "limit": 20
}
```

Response includes `nextCursor` if more results exist:
```json
{
  "servers": [ /* 20 servers */ ],
  "metadata": {
    "count": 20,
    "nextCursor": "eyJwYWdlIjogMn0=",
    "pagesRead": 1,
    "timeElapsed": "450ms"
  }
}
```

2. **Continue fetching** with cursor:
```json
{
  "query": "database",
  "cursor": "eyJwYWdlIjogMn0=",
  "limit": 20
}
```

3. **Repeat until** `nextCursor` is empty (no more results)

**Response Behavior:**
- `metadata.nextCursor` present → More results available, make another call
- `metadata.nextCursor` empty/absent → All results fetched
- `metadata.count` → Number of servers in THIS response
- `metadata.pagesRead` → API pages fetched for THIS call

**Performance:**
- Default (limit: 20): ~300-500ms per call ✅
- Higher limits: Proportionally longer
- Agents can balance speed vs completeness

**Example Queries:**

*Quick search (single page):*
```json
{
  "query": "database"
}
```

*Find database servers with high engagement:*
```json
{
  "query": "database",
  "min_stars": 50,
  "sort_by": "stars"
}
```

*Find stdio servers with specific tools:*
```json
{
  "transport": "stdio",
  "tools": ["query", "search"],
  "limit": 10
}
```

**Response Format:**
Returns enhanced `ServerListResponse` with chunking metadata:

```json
{
  "servers": [
    {
      "server": {
        "name": "io.example/database-server",
        "version": "2.1.0",
        "description": "Database management server",
        "packages": [...],
        "meta": {...}
      },
      "_meta": {}
    }
    // ... more servers
  ],
  "metadata": {
    "count": 534,
    "truncated": false,
    "pagesRead": 6,
    "timeElapsed": "18.5s"
  }
}
```

**Metadata Fields:**
- `count`: Total servers returned after filtering
- `truncated`: `true` if timeout hit and results are incomplete
- `pagesRead`: Number of API pages fetched
- `timeElapsed`: Total time taken

**Why Unified Tool:**
- ✅ No pagination complexity for agents
- ✅ Comprehensive filtering (10+ criteria)
- ✅ Handles 500+ servers gracefully
- ✅ Timeout protection prevents failures
- ✅ Single tool for all search/list needs

#### 2. `get_server_details`

Get comprehensive information about a specific MCP server.

**Parameters:**
- `server_name` (string, required): Fully qualified server name
- `version` (string, optional): Specific version or "latest" (default)

**Example Query:**
```json
{
  "server_name": "io.github.stacklok/everything",
  "version": "latest"
}
```

**Response Format:**
Returns a `ServerResponse` with complete server details:

```json
{
  "server": {
    "name": "io.github.stacklok/everything",
    "version": "1.2.3",
    "description": "Comprehensive server with multiple capabilities",
    "repository": {
      "url": "https://github.com/stacklok/everything",
      "source": "github"
    },
    "packages": [
      {
        "registryType": "npm",
        "identifier": "@stacklok/everything",
        "runTimeHint": "node",
        "transport": {"type": "stdio"}
      }
    ],
    "meta": {
      "io.modelcontextprotocol.registry/publisher-provided": {
        "io.github.stacklok": {
          "everything": {
            "metadata": {
              "stars": 150,
              "pulls": 1200
            },
            "tools": ["search", "analyze", "generate"],
            "tags": ["database", "ai", "search"]
          }
        }
      }
    }
  },
  "_meta": {}
}
```

#### 3. `compare_servers`

Compare multiple MCP servers side-by-side.

**Parameters:**
- `server_names` (array, required): List of 2-5 server names to compare

**Example Query:**
```json
{
  "server_names": [
    "io.github.stacklok/database",
    "io.github.stacklok/sql"
  ]
}
```

**Note:** This tool returns a custom formatted text comparison table for better readability, while other tools return official JSON format.

#### 4. `get_setup_guide`

Get comprehensive installation and configuration instructions for an MCP server.

**Parameters:**
- `server_name` (string, required): Server to get setup guide for
- `platform` (string, optional): Platform: "claude-desktop", "cursor", "custom" (default: "claude-desktop")
- `runtime` (string, optional): Runtime: "node", "python", "docker" (auto-detected if not specified)

**Example Query:**
```json
{
  "server_name": "io.example/postgres-mcp",
  "platform": "claude-desktop"
}
```

**Response:** Returns a comprehensive markdown setup guide including:
- Prerequisites (runtime, transport type)
- Step-by-step installation instructions
- Environment variable configuration
- Platform-specific config examples (Claude Desktop, Cursor, custom)
- Troubleshooting tips
- Links to documentation and repository

**Use Cases:**
- First-time server installation
- Switching between platforms (Claude Desktop ↔ Cursor)
- Troubleshooting installation issues
- Understanding environment variable requirements

#### 5. `find_alternatives`

Find alternative MCP servers with similar capabilities using heuristic similarity scoring.

**Parameters:**
- `server_name` (string, required): Find alternatives to this server
- `reason` (string, optional): Why looking for alternative: "deprecated", "license", "features", "performance"
- `limit` (number, optional): Max alternatives (default: 5, max: 20)

**Example Query:**
```json
{
  "server_name": "io.example/postgres-mcp",
  "limit": 5
}
```

**Response:** Returns JSON with similarity-ranked alternatives:
```json
{
  "alternatives": [
    {
      "server": { /* ServerResponse */ },
      "similarityScore": 0.85,
      "matchReasons": ["shared tags: database, sql", "similar tools: 8/10"],
      "migrationComplexity": "Low",
      "differences": ["transport: stdio vs http"]
    }
  ],
  "metadata": {
    "count": 5,
    "sourceServer": "io.example/postgres-mcp",
    "scoringCriteria": "tags(40%), tools(40%), transport(10%), description(10%)"
  }
}
```

**Similarity Scoring:**
- **Tags (40%)**: Shared tags indicate similar use cases (uses [Jaccard similarity](https://en.wikipedia.org/wiki/Jaccard_index): intersection/union)
- **Tools (40%)**: Tool overlap shows functional equivalence (uses [Jaccard similarity](https://en.wikipedia.org/wiki/Jaccard_index): intersection/union)
- **Transport (10%)**: Same transport means easier migration (binary match: 1.0 or 0.0)
- **Description (10%)**: Keyword matching in descriptions (overlap coefficient)

**Migration Complexity:**
- **Low**: High tool overlap (≥80%), easy to switch
- **Medium**: Moderate tool overlap (50-80%), some code changes needed
- **High**: Low tool overlap (<50%), significant code changes required

**Use Cases:**
- Evaluating alternative implementations
- Planning migration from deprecated servers
- Finding feature-rich alternatives
- Exploring the ecosystem

### Journey 2: MCP Developer Tools

#### 6. `find_similar_servers`

Find servers with similar capabilities for market research and competition analysis.

**Parameters:**
- `server_name` (string, optional): Find servers similar to this one
- `tags` (array, optional): Find servers with these tags
- `tools` (array, optional): Find servers with these tools
- `limit` (number, optional): Max results (default: 10, max: 50)

**Example Queries:**

*Find servers similar to a specific server:*
```json
{
  "server_name": "io.example/my-server",
  "limit": 10
}
```

*Find servers by capabilities:*
```json
{
  "tags": ["database", "sql"],
  "tools": ["query", "execute"],
  "limit": 20
}
```

**Response:** Returns JSON with similarity-ranked servers:
```json
{
  "servers": [
    {
      "server": { /* ServerResponse */ },
      "similarityScore": 0.82,
      "matchReasons": ["tags: database, sql", "tools: 2/2"]
    }
  ],
  "metadata": {
    "count": 10,
    "searchCriteria": "similar to io.example/my-server"
  }
}
```

**Use Cases:**
- Market research and competitive analysis
- Discovering similar implementations
- Finding servers in the same category
- Understanding the competitive landscape

#### 7. `get_server_analytics`

Get analytics and popularity metrics for your MCP server.

**Parameters:**
- `server_name` (string, required): Server to analyze
- `period` (string, optional): Time period: "7d", "30d", "90d", "all" (default: "30d")

**Example Query:**
```json
{
  "server_name": "io.example/my-server",
  "period": "30d"
}
```

**Response:** Returns comprehensive analytics:
```json
{
  "serverName": "io.example/my-server",
  "period": "30d",
  "current": {
    "stars": 150,
    "pulls": 500,
    "toolCount": 5,
    "tags": ["database", "sql"]
  },
  "popularity": {
    "rank": "Medium",
    "percentile": "Top 40%",
    "comparedTo": "all registered MCP servers"
  },
  "recommendations": [
    "Consider adding more descriptive tags to improve discoverability"
  ]
}
```

**Metrics Provided:**
- Current stars, pulls, tool count
- Popularity ranking and percentile
- Actionable recommendations for improvement

**Note:** Time-series trend data will be available when historical tracking is implemented.

**Use Cases:**
- Track server adoption and growth
- Understand your server's market position
- Get recommendations for improvement
- Benchmark against the ecosystem

#### 8. `get_ecosystem_insights`

Get insights about the MCP ecosystem or a specific category.

**Parameters:**
- `category` (string, optional): Category to analyze: "database", "files", "api", "all" (default: "all")

**Example Queries:**

*Analyze entire ecosystem:*
```json
{
  "category": "all"
}
```

*Analyze database category:*
```json
{
  "category": "database"
}
```

**Response:** Returns ecosystem statistics and insights:
```json
{
  "category": "all",
  "overview": {
    "totalServers": 250,
    "totalStars": 15000,
    "totalPulls": 45000,
    "avgStars": 60,
    "avgPulls": 180
  },
  "topTags": [
    {"name": "database", "count": 45},
    {"name": "api", "count": 38}
  ],
  "topTools": [
    {"name": "query", "count": 52},
    {"name": "search", "count": 41}
  ],
  "transports": [
    {"name": "stdio", "count": 150},
    {"name": "http", "count": 75}
  ],
  "runtimes": [
    {"name": "node", "count": 120},
    {"name": "python", "count": 85}
  ],
  "insights": [
    "Most popular transport: stdio (150 servers)",
    "Most common runtime: node (120 servers)"
  ],
  "opportunities": [
    "Areas with fewer than 5 servers represent opportunities"
  ]
}
```

**Insights Provided:**
- Total servers, stars, and pulls in category
- Most popular tags, tools, transports, runtimes
- Average engagement metrics
- Underserved areas and opportunities

**Use Cases:**
- Strategic planning for new servers
- Identify market gaps and opportunities
- Understand ecosystem trends
- Category-specific analysis

#### 9. `analyze_tool_overlap`

Analyze tool overlap between multiple servers to identify complementary vs competing servers.

**Parameters:**
- `server_names` (array, required): Servers to analyze (2-10 servers)
- `show_unique` (boolean, optional): Show unique tools per server (default: true)

**Example Query:**
```json
{
  "server_names": [
    "io.example/server1",
    "io.example/server2",
    "io.example/server3"
  ],
  "show_unique": true
}
```

**Response:** Returns overlap analysis:
```json
{
  "servers": [
    {
      "serverName": "io.example/server1",
      "totalTools": 5,
      "uniqueTools": ["special_query"]
    }
  ],
  "overlapMatrix": [
    {
      "serverA": "io.example/server1",
      "serverB": "io.example/server2",
      "overlapScore": 0.67,
      "sharedTools": 2
    }
  ],
  "summary": {
    "totalServers": 3,
    "totalUniqueTools": 8,
    "avgOverlap": 0.45
  },
  "insights": [
    "Moderate overlap - some shared functionality with unique features",
    "Highest overlap: server1 ↔ server2 (67% similar, 2 shared tools)"
  ]
}
```

**Analysis Provided:**
- Overlap matrix (Jaccard similarity scores)
- Unique tools per server
- Shared tool counts
- Insights on complementary vs competing servers

**Use Cases:**
- Identify competing servers
- Find complementary servers for partnerships
- Avoid feature duplication
- Plan server differentiation strategy

### When to Use Each Tool

Choose the right tool for your use case:

| Need | Tool | Journey | Reason |
|------|------|---------|--------|
| Quick search, preview results | `search_servers` (no cursor) | 1 (Consumer) | Fast, 20 results, <500ms |
| Filtered complete results | `search_servers` (iterate with cursor) | 1 (Consumer) | Controlled pagination, agent-friendly |
| ALL servers, no filter | `search_servers` (iterate to completion) | 1 (Consumer) | Use cursor to fetch all pages |
| Single page with many filters | `search_servers` + `limit` | 1 (Consumer) | One-shot filtered query |
| Detailed info on one server | `get_server_details` | 1 (Consumer) | Full metadata, packages, tags |
| Side-by-side comparison | `compare_servers` | 1 (Consumer) | Compare features across 2-5 servers |
| Installation instructions | `get_setup_guide` | 1 (Consumer) | Step-by-step setup, platform configs, troubleshooting |
| Find migration alternatives | `find_alternatives` | 1 (Consumer) | Discover alternatives with similarity scores |
| Market research | `find_similar_servers` | 2 (Developer) | Find competing/similar servers with match reasons |
| Track server performance | `get_server_analytics` | 2 (Developer) | Stars, pulls, popularity rank, recommendations |
| Understand ecosystem | `get_ecosystem_insights` | 2 (Developer) | Category statistics, top tags/tools, opportunities |
| Analyze competition | `analyze_tool_overlap` | 2 (Developer) | Tool overlap matrix, unique features, insights |

### Response Format

All tools (except `compare_servers`) return responses in the **official MCP Registry API format** as defined at [registry.modelcontextprotocol.io](https://registry.modelcontextprotocol.io/).

#### ServerListResponse

Used by `search_servers`:

```json
{
  "servers": [
    {
      "server": {
        "$schema": "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
        "name": "io.example/my-server",
        "version": "1.0.0",
        "description": "Server description",
        "repository": {
          "url": "https://github.com/example/my-server",
          "source": "github"
        },
        "packages": [...],
        "meta": {...}
      },
      "_meta": {
        "publishedAt": "2025-01-15T10:00:00Z"
      }
    }
  ],
  "metadata": {
    "count": 20,
    "nextCursor": "eyJwYWdlIjogMn0=",
    "pagesRead": 1,
    "timeElapsed": "450ms"
  }
}
```

**Fields:**
- `servers[]`: Array of server entries
- `servers[].server`: Full server configuration (ServerJSON format)
- `servers[]._meta`: Registry-managed metadata
- `metadata.count`: Number of items in current page/response
- `metadata.nextCursor`: Pagination cursor (present only if more results exist)
- `metadata.pagesRead`: Number of API pages fetched (search_servers only)
- `metadata.timeElapsed`: Time taken for the request (search_servers only)

#### ServerResponse

Used by `get_server_details`:

```json
{
  "server": {
    "$schema": "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
    "name": "io.example/my-server",
    "version": "1.0.0",
    "description": "Comprehensive server description",
    "title": "My Server",
    "repository": {
      "url": "https://github.com/example/my-server",
      "source": "github"
    },
    "packages": [
      {
        "registryType": "npm",
        "identifier": "@example/my-server",
        "runTimeHint": "node",
        "transport": {
          "type": "stdio"
        }
      }
    ],
    "meta": {
      "io.modelcontextprotocol.registry/publisher-provided": {
        "io.github.example": {
          "my-server": {
            "metadata": {
              "stars": 100,
              "pulls": 500
            },
            "tools": ["search", "query"],
            "tags": ["database", "search"]
          }
        }
      }
    }
  },
  "_meta": {
    "publishedAt": "2025-01-15T10:00:00Z",
    "updatedAt": "2025-01-20T14:30:00Z"
  }
}
```

### Pagination

The MCP server implements **cursor-based pagination** following the [MCP specification](https://modelcontextprotocol.io/specification/draft/server/utilities/pagination).

#### How Pagination Works

1. **Initial Request**: Call `search_servers` without a cursor
   ```json
   {
     "name": "search_servers",
     "arguments": {
       "limit": 20
     }
   }
   ```

2. **Response with Cursor**: Server returns results and a `nextCursor` if more results exist
   ```json
   {
     "servers": [...],
     "metadata": {
       "count": 20,
       "nextCursor": "eyJwYWdlIjogMn0="
     }
   }
   ```

3. **Subsequent Requests**: Pass the `nextCursor` to get the next page
   ```json
   {
     "name": "search_servers",
     "arguments": {
       "cursor": "eyJwYWdlIjogMn0=",
       "limit": 20
     }
   }
   ```

4. **End of Results**: When `nextCursor` is absent or empty, you've reached the end

#### Important Notes

- **Opaque Cursors**: Treat cursors as opaque tokens
  - Do NOT parse cursor contents
  - Do NOT modify cursor values
  - Do NOT persist cursors across sessions
- **Invalid Cursors**: Using an invalid cursor returns error code `-32602` (Invalid params)
- **Page Size**: The server determines actual page size; requested `limit` is a hint
- **Stable Results**: Cursors provide stable pagination even as data changes

## Usage

### Prerequisites

The MCP server can connect to either:

1. **Official MCP Registry** at https://registry.modelcontextprotocol.io (no setup needed)
2. **Local Registry API server** for custom registries

For local Registry API:
```bash
# Start the Registry API server (with data sync)
thv-registry-api serve \
  --config examples/config-file.yaml \
  --address :8080
```

### Standalone Mode

#### HTTP Transport (Default)

Start the MCP server connecting to the Registry API:

```bash
thv-registry-mcp serve \
  --registry-url http://localhost:8080 \
  --address :8081
```

Test with curl:

```bash
# Initialize connection
curl -X POST http://localhost:8081 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2024-11-05",
      "capabilities": {},
      "clientInfo": {"name": "test-client", "version": "1.0.0"}
    }
  }'

# List available tools
curl -X POST http://localhost:8081 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/list"
  }'

# Search for servers
curl -X POST http://localhost:8081 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
      "name": "search_servers",
      "arguments": {
        "query": "database tool",
        "limit": 5
      }
    }
  }'
```

#### Stdio Transport

For direct MCP client integration (Claude Desktop, Cursor, etc.):

```bash
thv-registry-mcp serve \
  --registry-url http://localhost:8080 \
  --transport stdio
```

This mode is ideal for integration with MCP clients like Claude Desktop or other AI assistants that support stdio-based MCP connections.

**Note**: The Registry API server must be running and accessible at the specified URL.

### Integrated Mode

Enable MCP endpoints directly in the Registry API server (no separate process needed):

```bash
thv-registry-api serve \
  --config examples/config-file.yaml \
  --address :8080 \
  --enable-mcp
```

MCP endpoints will be available at:
- `POST /mcp` - Standard JSON-RPC endpoint (SDK StreamableHTTP handler)

This mode is useful when you want a single process handling both REST and MCP protocols.

### Example: Natural Language Queries

The MCP server excels at handling natural language queries:

**Query 1: "What tool should I use for connecting to an Oracle DB?"**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "search_servers",
    "arguments": {
      "query": "oracle database connector",
      "limit": 3
    }
  }
}
```

**Query 2: "What's the database tool with the most stars?"**

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "search_servers",
    "arguments": {
      "query": "database",
      "sort_by": "stars",
      "limit": 1
    }
  }
}
```

**Query 3: "Compare the file system tools"**

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "search_servers",
    "arguments": {
      "query": "filesystem",
      "tags": ["files", "storage"]
    }
  }
}
```

## User Journeys

This section demonstrates complete workflows for different user personas using the MCP tools.

### Journey 1: New MCP Consumer

**Scenario**: A developer needs to connect to PostgreSQL and wants to find, evaluate, install, and explore alternatives.

#### Step 1: Discovery

*"I need to query a PostgreSQL database"*

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "search_servers",
    "arguments": {
      "query": "postgresql database",
      "limit": 5,
      "sort_by": "stars"
    }
  }
}
```

**Response:** List of 5 postgres-related servers sorted by popularity...

#### Step 2: Detailed Information

*"Tell me about this postgres server"*

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "get_server_details",
    "arguments": {
      "server_name": "io.example/postgres-mcp"
    }
  }
}
```

**Response:** Complete server details including packages, tools, metadata, repository info...

#### Step 3: Installation Guide

*"How do I install it in Claude Desktop?"*

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "get_setup_guide",
    "arguments": {
      "server_name": "io.example/postgres-mcp",
      "platform": "claude-desktop"
    }
  }
}
```

**Response:** Comprehensive markdown guide with:
- Prerequisites and runtime detection
- Installation commands (`npm install`, `npx` usage)
- Environment variables (DATABASE_URL with examples)
- Claude Desktop config snippet
- Alternative platform configs (Cursor, custom)
- Troubleshooting tips
- Links to documentation

#### Step 4: Explore Alternatives

*"What are other postgres tools I should consider?"*

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "tools/call",
  "params": {
    "name": "find_alternatives",
    "arguments": {
      "server_name": "io.example/postgres-mcp",
      "limit": 5
    }
  }
}
```

**Response:** JSON with ranked alternatives:
```json
{
  "alternatives": [
    {
      "server": {
        "name": "io.example/postgresql-connector",
        "description": "Enhanced PostgreSQL MCP connector"
      },
      "similarityScore": 0.87,
      "matchReasons": [
        "shared tags: database, postgres, sql",
        "similar tools: 9/10",
        "same transport: stdio"
      ],
      "migrationComplexity": "Low",
      "differences": ["more features: includes query builder"]
    }
  ],
  "metadata": {
    "count": 5,
    "sourceServer": "io.example/postgres-mcp",
    "scoringCriteria": "tags(40%), tools(40%), transport(10%), description(10%)"
  }
}
```

### Journey 2: MCP Developer

Tools for MCP server authors and developers:

#### Example Queries

**Market Research:**
> "What other database MCP servers exist? Show me similar implementations."

Uses: `find_similar_servers` with tags/tools, or by reference server

**Track Your Server:**
> "How is my server 'io.example/my-db-server' performing? What's its popularity rank?"

Uses: `get_server_analytics`

**Ecosystem Analysis:**
> "What's the landscape for database MCP servers? Are there opportunities?"

Uses: `get_ecosystem_insights` with category filter

**Competitive Analysis:**
> "Compare tool overlap between my server and the top 3 database servers. What unique features do I have?"

Uses: `analyze_tool_overlap` with your server and competitors

**Feature Planning:**
> "What tools are most common in successful database servers?"

Uses: `get_ecosystem_insights` for top tools by category

#### MCP Tool Flow for Journey 2

```
Developer Question
       ↓
1. find_similar_servers → Discover competitors
       ↓
2. get_server_analytics → Track performance
       ↓
3. get_ecosystem_insights → Understand market
       ↓
4. analyze_tool_overlap → Identify differentiation
```

**Key Features:**
- Similarity scoring based on tags, tools, and metadata
- Derived analytics from current snapshot data
- Ecosystem-wide statistics and trends
- Tool overlap analysis with unique feature detection
- Actionable insights and recommendations

### Journey 3: Power User (Future)

**Coming soon** - Tools for workflow builders:
- `suggest_workflow` - Curated server combinations for use cases
- `get_integration_examples` - Code snippets for multi-server workflows
- `assess_server_quality` - Quality scoring for decision-making
- `check_compatibility` - Runtime/platform compatibility matrix

## ToolHive Metadata

The MCP server exposes rich ToolHive-specific metadata for each server:

```json
{
  "tier": "Community | Official | Enterprise",
  "status": "Active | Deprecated | Experimental",
  "tools": ["tool1", "tool2", "..."],
  "metadata": {
    "stars": 72067,
    "pulls": 17019,
    "last_updated": "2025-11-07T02:32:30Z"
  },
  "transport": "stdio | http | sse",
  "permissions": {...},
  "repository_url": "https://github.com/..."
}
```

This metadata powers intelligent search, ranking, and comparison features.

## Configuration

The MCP server is stateless and connects to an existing Registry API server. Configuration is minimal:

### Command-line Flags

```bash
thv-registry-mcp serve [flags]

Flags:
  --registry-url string    URL of the Registry API server (required)
                          Example: http://localhost:8080
  --address string        Address to listen on for HTTP mode (default ":8081")
  --transport string      Transport mode: "http" or "stdio" (default "http")
  --help                  Show help message
```

### Environment Variables

```bash
# Registry API connection
export REGISTRY_URL=http://localhost:8080

# Start MCP server
thv-registry-mcp serve
```

### Example Configurations

**Official MCP Registry (Public)**:
```bash
thv-registry-mcp serve --registry-url https://registry.modelcontextprotocol.io
```

**Local Development**:
```bash
thv-registry-mcp serve --registry-url http://localhost:8080
```

**Docker Compose**:
```bash
thv-registry-mcp serve --registry-url http://registry-api:8080
```

**Production (Kubernetes)**:
```bash
thv-registry-mcp serve --registry-url http://thv-registry-api.toolhive.svc.cluster.local
```

**Note**: All registry data synchronization and source configuration is handled by the Registry API server (`thv-registry-api`). See the main [README.md](README.md) for Registry API configuration options.

## Integration with AI Assistants

### Claude Desktop

Add to your Claude Desktop configuration (`~/.config/claude/config.json` or equivalent):

**Option 1: Connect to Official MCP Registry**
```json
{
  "mcpServers": {
    "toolhive-registry": {
      "command": "/path/to/bin/thv-registry-mcp",
      "args": [
        "serve",
        "--registry-url", "https://registry.modelcontextprotocol.io",
        "--transport", "stdio"
      ]
    }
  }
}
```

**Option 2: Connect to Local Registry API**
```json
{
  "mcpServers": {
    "toolhive-registry": {
      "command": "/path/to/bin/thv-registry-mcp",
      "args": [
        "serve",
        "--registry-url", "http://localhost:8080",
        "--transport", "stdio"
      ]
    }
  }
}
```

### Cursor

Add to your Cursor MCP configuration (`~/.cursor/mcp.json`):

**Option 1: Connect to Official MCP Registry**
```json
{
  "mcpServers": {
    "toolhive-registry": {
      "command": "/path/to/bin/thv-registry-mcp",
      "args": ["serve", "--registry-url", "https://registry.modelcontextprotocol.io", "--transport", "stdio"]
    }
  }
}
```

**Option 2: Connect to Local Registry API**
```json
{
  "mcpServers": {
    "toolhive-registry": {
      "command": "/path/to/bin/thv-registry-mcp",
      "args": ["serve", "--registry-url", "http://localhost:8080", "--transport", "stdio"]
    }
  }
}
```

### Custom MCP Clients

For custom MCP clients, connect via HTTP:

```javascript
const response = await fetch('http://localhost:8081', {
  method: 'POST',
  headers: {'Content-Type': 'application/json'},
  body: JSON.stringify({
    jsonrpc: '2.0',
    id: 1,
    method: 'tools/call',
    params: {
      name: 'search_servers',
      arguments: {query: 'database', limit: 5}
    }
  })
});
```

## API Reference

### JSON-RPC Methods

| Method | Description |
|--------|-------------|
| `initialize` | Initialize MCP connection |
| `tools/list` | List available tools |
| `tools/call` | Execute a tool |
| `resources/list` | List resources (empty) |

### Tool Schemas

All tools use JSON Schema for input validation. See the `tools/list` response for complete schemas.

## Development

### Building

```bash
# Build standalone MCP server
go build -o bin/thv-registry-mcp ./cmd/thv-registry-mcp

# Build API server with MCP support
go build -o bin/thv-registry-api ./cmd/thv-registry-api

# Build both
task build-all
```

### Testing

```bash
# Run tests
go test ./internal/mcp/...

# Run with coverage
go test -cover ./internal/mcp/...
```

### Adding New Tools

With the Go SDK, adding tools is straightforward:

1. **Define parameter struct** in `tools.go` with `jsonschema` tags:
```go
type MyToolParams struct {
    Query string `json:"query" jsonschema:"Search query"`
    Limit int    `json:"limit,omitempty" jsonschema:"Maximum results (default: 10)"`
}
```

2. **Implement handler function** with SDK signature:
```go
func (s *Server) myTool(ctx context.Context, req *sdkmcp.CallToolRequest, params *MyToolParams) (*sdkmcp.CallToolResult, any, error) {
    // Call Registry API
    // Format response
    return &sdkmcp.CallToolResult{
        Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: result}},
    }, nil, nil
}
```

3. **Register tool** in `server.go`:
```go
sdkmcp.AddTool(s.sdkServer, &sdkmcp.Tool{
    Name:        "my_tool",
    Description: "Description of what it does",
}, s.myTool)
```

4. **Add tests** in `tools_test.go`

The SDK automatically generates JSON schemas from your Go structs!

## Troubleshooting

### Connection Issues

**Problem**: MCP client can't connect

**Solutions**:
1. Verify the MCP server is running:
```bash
ps aux | grep thv-registry-mcp
```

2. Verify the Registry API is accessible:
```bash
curl http://localhost:8080/v0/servers
```

3. Check connectivity:
```bash
curl -X POST http://localhost:8081 -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'
```

### Tool Execution Errors

**Problem**: Tool returns errors

**Solution**: Check the tool arguments match the schema:
```bash
# List tools to see schemas
curl -X POST http://localhost:8081 -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

### Empty Results

**Problem**: Search returns no results

**Solution**: 
1. Verify the Registry API has data:
```bash
curl http://localhost:8080/v0/servers | jq length
```
2. Check if Registry API sync is working:
```bash
curl http://localhost:8080/health
```
3. Try broader search terms
4. Remove tag filters

## Performance

- **Cold start**: < 500ms (stateless, no data loading)
- **Search latency**: < 50ms + Registry API response time
- **Memory footprint**: ~30MB (no registry data cached)
- **Code size**: ~470 lines (56% reduction from custom implementation)
- **Concurrent connections**: Tested up to 1000 simultaneous
- **Scalability**: Horizontally scalable (stateless architecture)

## Security

- No authentication by default (use reverse proxy if needed)
- Read-only access to registry data
- No write operations exposed via MCP
- Sandboxed tool execution

## Implementation Notes

### Architecture Decisions

**Stateless Design**: The MCP server doesn't manage registry data, enabling:
- Zero data synchronization overhead
- Instant startup times
- Horizontal scalability
- Simplified deployment (single binary, no storage)
- No sync configuration needed

**SDK Benefits**:
- **Reduced Maintenance**: SDK handles MCP protocol updates automatically
- **Type Safety**: JSON schemas generated from Go structs, catching errors at compile time
- **Standards Compliance**: Guaranteed compatibility with MCP clients
- **Less Code**: 56% reduction in code size (1,100 lines → 470 lines)
- **Better Testing**: SDK provides testing utilities and handles protocol edge cases

**Registry API Client**: All data operations proxy to the Registry API:
- `search_servers` → `GET /v0/servers?filter=...&sort=...&limit=...`
- `get_server_details` → `GET /v0/servers/{name}`
- `compare_servers` → Multiple `GET /v0/servers/{name}` calls

### Migration History

**Version 1.0.0**: Migrated from custom MCP implementation to official SDK:
- Automatic schema validation
- Simplified transport handling
- Reduced technical debt
- Future-proof architecture

**Version 2.0.0**: Refactored to stateless client architecture:
- Changed from `--config` to `--registry-url`
- Removed data source management
- Proxy pattern for Registry API calls
- Improved startup performance

## Roadmap

- [ ] Semantic search using embeddings
- [ ] Server recommendation engine  
- [ ] Real-time registry updates
- [ ] Custom tool plugins
- [ ] Multi-registry support
- [ ] GraphQL interface

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.

## Support

- Issues: https://github.com/stacklok/toolhive-registry-server/issues
- Discussions: https://github.com/stacklok/toolhive-registry-server/discussions
- Docs: https://github.com/stacklok/toolhive-registry-server/tree/main/docs

