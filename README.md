# url2mcp — URL to MCP Server

A Go application that lets you register URLs, fetch and parse their content, and expose them as resources over the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/). Two servers in one binary:

- **Web UI** on `:8080` — register URLs, manage resources, preview extracted content (title, plain text, links)
- **MCP Server** on `:8081` — expose registered URLs as MCP resources over SSE transport for any MCP client

## Quick Start

```bash
go run .
```

Then:

1. Open **http://localhost:8080** in a browser.
2. Register a URL — e.g. name=`hn`, url=`https://news.ycombinator.com`.
3. The resource list shows the entry; click it to preview the extracted title, plain text, and links.
4. Connect an MCP client to **http://localhost:8081/sse**.
5. Send `resources/list` → see `url2mcp://hn` in the response.
6. Send `resources/read` with `{"uri": "url2mcp://hn"}` → get back plain text, HTML body, and links array.

## Architecture

```
                    ┌──────────────┐
                    │  Web UI      │  :8080
                    │  (static UI  │
  Browser ─────────►│   + REST     │
                    │   API)       │
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │   In-Memory  │
                    │   Store      │  (thread-safe, sync.RWMutex)
                    │  (name →     │
                    │   PageContent)│
                    └──────┬───────┘
                           │
  MCP Client ─────►┌──────▼───────┐
                   │  MCP Server  │  :8081
                   │  (SSE +      │
                   │   JSON-RPC)  │
                   └──────────────┘
```

## Features

### URL Fetching & Parsing

When you register a URL, url2mcp downloads the page and parses it into three representations:

- **Plain text** (`BodyText`) — extracted text with `<script>`/`<style>` content stripped, block elements separated by newlines, and whitespace collapsed
- **HTML body** (`BodyHTML`) — the raw inner-HTML of the `<body>` tag, faithfully reconstructed with proper attribute escaping
- **Links** (`Links`) — deduplicated list of all `<a href="...">` values, with relative URLs resolved against the page's base URL, excluding `#` fragments and `javascript:` links

HTTP requests use a realistic User-Agent header, a 15-second timeout, and full Accept headers for compatibility.

### Web UI (`:8080`)

A dark-themed single-page application for managing URL resources:

- **Register URL form** — enter a name and URL, click Register
- **Resource list** — shows all registered resources with name, URL, and a Remove button per entry
- **Preview panel** — click any resource to see its title, extracted plain text (up to 400px scrollable), and all extracted links (up to 50, with overflow count)

### REST API (`:8080`)

| Method | Path | Description |
|---|---|---|
| `GET` | `/` | Serves the web UI (`static/index.html`) |
| `POST` | `/api/register` | Register a URL — fetches, parses, and stores it |
| `GET` | `/api/resources` | List all registered resources (name + URL) |
| `GET` | `/api/resources/{name}/preview` | Get preview data (title, body_text, links) |
| `DELETE` | `/api/resources/{name}` | Remove a resource |

### MCP Server (`:8081`)

Implements JSON-RPC 2.0 over SSE transport per the MCP specification (`2024-11-05`). Two endpoints:

| Endpoint | Description |
|---|---|
| `GET /sse` | SSE stream — sends `endpoint` event with session ID, then streams JSON-RPC responses |
| `POST /message?sessionId=...` | Send JSON-RPC requests bound to the SSE session |

#### Supported MCP Methods

| Method | Description |
|---|---|
| `initialize` | Protocol handshake — returns server name (`url2mcp`), version, protocol version, and capabilities |
| `notifications/initialized` | Acknowledged (no-op) |
| `resources/list` | List all registered URL resources as `url2mcp://<name>` URIs |
| `resources/read` | Read content of a specific resource by URI |

#### Resource URIs

| URI | MIME Type | Content |
|---|---|---|
| `url2mcp://<name>` | `text/plain` | Plain text body |
| `url2mcp://<name>#html` | `text/html` | Raw HTML body |
| `url2mcp://<name>#links` | `application/json` | JSON array of all links |

When reading the base URI (no fragment), the server returns **all three** content representations in a single response.

### CLI Client (`cmd/mcp-client`)

A command-line MCP client that speaks directly to the SSE endpoint — useful for testing and scripting:

```bash
# Build
go build -o mcp-client ./cmd/mcp-client

# List all resources
./mcp-client --list

# Read a resource (returns all: text, html, links)
./mcp-client --resource hn

# Read a specific subtype
./mcp-client --resource hn --subtype html
./mcp-client --resource hn --subtype links

# Point at a different MCP server
./mcp-client --server http://other-host:8081 --resource hn
```

Flags:

| Flag | Default | Description |
|---|---|---|
| `--server` | `http://localhost:8081` | MCP server base URL |
| `--resource` | `""` | Resource name to read |
| `--subtype` | `""` | Resource subtype: `text`, `html`, or `links` (default: all) |
| `--list` | `false` | List all available resources |

### Makefile

| Target | Description |
|---|---|
| `build` | Build both `url2mcp` and `mcp-client` binaries |
| `start` | Build and start the server in background (saves PID) |
| `stop` | Stop the server |
| `run` | Build and run the server in foreground |
| `test` | Run all tests with the race detector (`go test -race -v ./...`) |
| `clean` | Remove built binaries |

## Build

```bash
# Build both binaries
make build

# Or build individually
go build -o url2mcp .
go build -o mcp-client ./cmd/mcp-client
```

## Tests

```bash
make test
```

All packages have comprehensive test coverage:

- **`internal/store`** — thread-safe map operations, concurrency safety
- **`internal/fetcher`** — HTML parsing, link extraction, relative URL resolution, script/style stripping, attribute escaping
- **`internal/mcp`** — JSON-RPC request dispatching, SSE session management, CORS handling
- **`internal/web`** — REST endpoint handling, registration flow, preview responses
- **`cmd/mcp-client`** — SSE connection, session extraction, full resource read and list flows

## Dependencies

- `golang.org/x/net` — HTML tokenizer/parser (the only external dependency)

Everything else is Go standard library: `net/http`, `encoding/json`, `sync`, `html`, `crypto/rand`, etc.
