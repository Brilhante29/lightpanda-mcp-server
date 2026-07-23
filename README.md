# 🐼 Lightpanda MCP Server

[![MCP Standard](https://img.shields.io/badge/MCP-2024--11--05-blue.svg)](https://modelcontextprotocol.io)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://golang.org)
[![NPM](https://img.shields.io/badge/NPM-npx_lightpanda--mcp--server-red?logo=npm)](https://npmjs.com)

A **100% project-agnostic, standalone Model Context Protocol (MCP) server** for [Lightpanda](https://lightpanda.io) — the ultra-fast AI-native headless browser (16x lower memory footprint than Chrome).

Connects **OpenCode, Claude Code, Codex, Antigravity, Cursor, Windsurf, or LangChain** to Lightpanda over standard MCP JSON-RPC 2.0.

---

## 🚀 3 Ways to Use (100% Project-Agnostic)

### 📦 Way 1: Global Go Install (`go install`)

Install globally to your system with a single command:

```bash
go install github.com/Brilhante29/lightpanda-mcp-server@latest
```

Then in **OpenCode, Claude Code, Codex, Cursor, or Antigravity**, your MCP configuration is **100% project-agnostic**:

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

### 📦 Way 2: Dynamic `go run` (GitHub Direct Execution)

Run directly from GitHub without installing anything locally:

```json
{
  "mcpServers": {
    "lightpanda": {
      "command": "go",
      "args": ["run", "github.com/Brilhante29/lightpanda-mcp-server@latest"]
    }
  }
}
```

---

### 📦 Way 3: Zero-Install NPX (`npx`)

Run via NPX:

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

## 🛠️ Provided MCP Tools

| Tool Name | Description | Parameters |
| :--- | :--- | :--- |
| `lightpanda_fetch_html` | Ultra-fast HTML extraction from any URL. | `url` (string) |
| `lightpanda_get_markdown` | Extracts clean Markdown & Accessibility Tree (AX Tree). | `url` (string) |
| `lightpanda_execute_js` | Evaluates JavaScript inside Lightpanda headless browser over CDP. | `url` (string), `script` (string) |
| `lightpanda_status` | Checks local Lightpanda daemon health and CDP WebSocket connectivity. | None |

---

## ⚙️ Environment Variables

- `LIGHTPANDA_HOST` (default: `127.0.0.1`): Host IP of local Lightpanda daemon.
- `LIGHTPANDA_PORT` (default: `9222`): Port of local Lightpanda CDP server.

---

## 📄 License

MIT License &copy; Guilherme Brilhante & Lightpanda Community.
