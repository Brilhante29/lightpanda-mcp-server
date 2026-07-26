# 🐼 Lightpanda MCP Server

[![MCP Standard](https://img.shields.io/badge/MCP-2024--11--05-blue.svg)](https://modelcontextprotocol.io)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![NPM](https://img.shields.io/badge/NPM-npx_lightpanda--mcp--server-red?logo=npm)](https://npmjs.com)
[![Bun](https://img.shields.io/badge/Bun-bunx_lightpanda--mcp--server-fbf0df?logo=bun)](https://bun.sh)
[![Go](https://img.shields.io/badge/Go-go_install-00ADD8?logo=go)](https://golang.org)
[![Python](https://img.shields.io/badge/Python-uvx_lightpanda--mcp--server-3776ab?logo=python)](https://pypi.org)

A **100% project-agnostic, multi-runtime Model Context Protocol (MCP) server** for [Lightpanda](https://lightpanda.io) — the ultra-fast AI-native headless browser (16x lower memory footprint than Chrome).

Supports **NPM, Bun, PNPM, Yarn, Go, Python/UVX**, and connects **OpenCode, Claude Code, Codex, Antigravity, Cursor, Windsurf, or LangChain** seamlessly!

---

## 📑 Changelog

### v1.1.0 — Robustness & cross-platform transport

- **fetchHTML** now respects HTTP status codes (errors on 4xx/5xx), follows redirects up to `LIGHTPANDA_MAX_REDIRECTS`, retries transient network errors with exponential backoff, and accepts a per-call `timeoutMs`.
- **executeJS** passes the user script via **stdin** instead of shell-interpolating it — eliminates a command-injection vector. Scripts may be either expressions or async functions receiving the Playwright `page`.
- **fetchMarkdown** is **cross-platform**: detects `wsl` on Windows and calls `lightpanda` directly on Linux/macOS. Falls back to raw HTML with a warning header when the CLI is unavailable.
- **Daemon launcher** is idempotent once-per-process and uses detached `spawn`/`unref` so the parent can exit cleanly.
- **Logs** are written to **stderr** with levels (`debug`/`info`/`warn`/`error`), configurable via `--log-level` or `LIGHTPANDA_LOG_LEVEL`.
- **CLI flags** added: `--host`, `--port`, `--timeout`, `--max-redirects`, `--max-retries`, `--log-level`.
- **MCP annotations** (`readOnlyHint`, `openWorldHint`) declared on every tool.
- **`logging/setLevel`** method acknowledged per MCP spec.

### v1.0.0 — initial release

Multi-runtime MCP server with `lightpanda_fetch_html`, `lightpanda_get_markdown`, `lightpanda_execute_js`, `lightpanda_status`.

---

## 🚀 Installation & Execution Methods

### ⚡ 1. NPM / NPX (Node.js)

```bash
# Zero-install execution via NPX
npx lightpanda-mcp-server

# Or global install
npm install -g lightpanda-mcp-server
```

**MCP Config:**
```json
{
  "mcpServers": {
    "lightpanda": {
      "command": "npx",
      "args": ["-y", "lightpanda-mcp-server"]
    }
  }
}
```

---

### 🥟 2. BUN / BUNX

```bash
# Zero-install execution via Bunx
bunx lightpanda-mcp-server

# Or global install via Bun
bun add -g lightpanda-mcp-server
```

**MCP Config:**
```json
{
  "mcpServers": {
    "lightpanda": {
      "command": "bunx",
      "args": ["lightpanda-mcp-server"]
    }
  }
}
```

---

### 📦 3. PNPM / DLX

```bash
pnpm dlx lightpanda-mcp-server
```

**MCP Config:**
```json
{
  "mcpServers": {
    "lightpanda": {
      "command": "pnpm",
      "args": ["dlx", "lightpanda-mcp-server"]
    }
  }
}
```

---

### 🧶 4. YARN / DLX

```bash
yarn dlx lightpanda-mcp-server
```

---

### 🐹 5. GO (`go install` / `go run`)

```bash
# Global Go install
go install github.com/Brilhante29/lightpanda-mcp-server@latest
```

**MCP Config:**
```json
{
  "mcpServers": {
    "lightpanda": {
      "command": "lightpanda-mcp-server"
    }
  }
}
```

---

### 🐍 6. PYTHON / UVX (`pip` / `uvx`)

```bash
uvx lightpanda-mcp-server
```

**MCP Config:**
```json
{
  "mcpServers": {
    "lightpanda": {
      "command": "uvx",
      "args": ["lightpanda-mcp-server"]
    }
  }
}
```

---

## 🛠️ Provided MCP Tools

| Tool Name | Description | Parameters |
| :--- | :--- | :--- |
| `lightpanda_fetch_html` | Ultra-fast HTML extraction from any URL. | `url` (string) |
| `lightpanda_get_markdown` | Extracts clean Markdown & Accessibility Tree (AX Tree). | `url` (string) |
| `lightpanda_execute_js` | Evaluates JavaScript inside Lightpanda headless browser over CDP. | `url` (string), `script` (string) |
| `lightpanda_status` | Checks local Lightpanda daemon health and CDP WebSocket connectivity. | None |

---

## ⚙️ Configuration

### Environment variables

- `LIGHTPANDA_HOST` (default: `127.0.0.1`): Host IP of local Lightpanda daemon.
- `LIGHTPANDA_PORT` (default: `9222`): Port of local Lightpanda CDP server.
- `LIGHTPANDA_HTTP_TIMEOUT` (default: `30000`): HTTP fetch timeout in ms.
- `LIGHTPANDA_MAX_REDIRECTS` (default: `5`): Maximum HTTP redirects to follow.
- `LIGHTPANDA_FETCH_RETRIES` (default: `2`): Retries on transient network errors.
- `LIGHTPANDA_LOG_LEVEL` (default: `info`): One of `debug` / `info` / `warn` / `error`.

> Node.js / Go binaries also accept equivalent flags: `--host`, `--port`, `--timeout`, `--max-redirects`, `--max-retries`, `--log-level`.

---

## 🔧 Tool annotations

Each tool declares MCP-standard annotations so clients can reason about safety:

| Tool | `readOnlyHint` | `openWorldHint` |
| :--- | :---: | :---: |
| `lightpanda_fetch_html` | ✅ | ✅ |
| `lightpanda_get_markdown` | ✅ | ✅ |
| `lightpanda_execute_js` | ❌ | ✅ |
| `lightpanda_status` | ✅ | ❌ |

---

## 📄 License

MIT License &copy; Guilherme Brilhante & Lightpanda Community.
