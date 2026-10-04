# Lightpanda MCP Server

A small, dependency-free [Model Context Protocol](https://modelcontextprotocol.io) server in Go that lets AI agents (Claude Code, Cursor, Windsurf, OpenCode, and other MCP clients) fetch pages and render them with [Lightpanda](https://lightpanda.io), a lightweight headless browser built for automation.

[![ci](https://github.com/Brilhante29/lightpanda-mcp-server/actions/workflows/ci.yml/badge.svg)](https://github.com/Brilhante29/lightpanda-mcp-server/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)

## Why this exists

Agents that browse the web need two things: the raw HTML of a page, and the page as a browser would see it after JavaScript runs. Full Chrome is heavy for that job. Lightpanda renders pages with a much smaller footprint, and this server exposes it to any MCP client over stdio with a deliberately small, safe surface:

- untrusted input never reaches a shell or an interpreter: URLs are validated (`http`/`https` with a host) and passed to the Lightpanda binary as a single argument;
- responses are size-limited and time-bounded;
- the server never starts background processes or opens network listeners on its own.

## Tools

| Tool | What it does | Parameters |
|---|---|---|
| `fetch_html` | Fetches raw HTML over HTTP. JavaScript is **not** executed. | `url` |
| `lightpanda_render_html` | Runs `lightpanda fetch --dump <url>` and returns the rendered HTML (JavaScript executed). | `url` |
| `lightpanda_status` | Checks whether a Lightpanda CDP server is reachable at `LIGHTPANDA_HOST:LIGHTPANDA_PORT`. | none |

## Install

Requires Go 1.22+. For rendering, install the Lightpanda binary from [lightpanda.io](https://lightpanda.io) and make sure it is on `PATH` (or set `LIGHTPANDA_BIN`).

```bash
go install github.com/Brilhante29/lightpanda-mcp-server@latest
```

The npm, Bun, and PyPI packages announced in earlier versions of this README were never published; Go is the supported distribution.

## Configure your MCP client

```json
{
  "mcpServers": {
    "lightpanda": {
      "command": "lightpanda-mcp-server",
      "env": { "LIGHTPANDA_BIN": "lightpanda" }
    }
  }
}
```

| Variable | Default | Purpose |
|---|---|---|
| `LIGHTPANDA_BIN` | `lightpanda` | Path or name of the Lightpanda binary used for rendering |
| `LIGHTPANDA_HOST` | `127.0.0.1` | Host checked by `lightpanda_status` |
| `LIGHTPANDA_PORT` | `9222` | CDP port checked by `lightpanda_status` |

If you run a Lightpanda CDP server for other tools, bind it to localhost (`lightpanda serve --host 127.0.0.1 --port 9222`): a CDP endpoint gives full control of the browser to anyone who can reach it.

## Development

```bash
gofmt -l .
go vet ./...
go test -race ./...
```

The tests cover URL validation, argument construction for the Lightpanda subprocess, HTTP fetching against a local server, and the JSON-RPC handling of tool calls, malformed lines, and unknown methods. CI runs the shared [reusable Go workflow](https://github.com/Brilhante29/ci-cd-templates).

## Security notes

Version 1.1 removed three behaviors from the first release:

- a JavaScript execution tool that interpolated the URL and script into Node.js source, which allowed code execution on the host;
- a Node.js implementation that passed URLs to a shell command;
- automatic launching of a Lightpanda daemon bound to `0.0.0.0`.

JavaScript execution may return later through a CDP client that sends the script as data, never as source code.

## Roadmap

- Markdown and accessibility-tree extraction from the rendered DOM.
- Script evaluation through CDP with explicit opt-in.
- Release binaries for Linux, macOS, and Windows.

## Author

**Guilherme Brilhante**, software engineer working on scalable backends and production AI.
[LinkedIn](https://www.linkedin.com/in/guilhermefreirebrilhanteseveriano/) · [GitHub](https://github.com/Brilhante29)

## License

[MIT](LICENSE). Lightpanda is a separate project with its own license.
