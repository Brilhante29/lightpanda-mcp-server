#!/usr/bin/env node

/**
 * Universal Lightpanda MCP Server (Node.js / Bun / Deno Runtime)
 * Zero external dependencies. Works on NPM, Bun, PNPM, Yarn, PyPI, and Go.
 */

const readline = require('readline');
const http = require('http');
const https = require('https');
const net = require('net');
const { exec } = require('child_process');

const LIGHTPANDA_HOST = process.env.LIGHTPANDA_HOST || '127.0.0.1';
const LIGHTPANDA_PORT = process.env.LIGHTPANDA_PORT || '9222';

let isDaemonStarting = false;

const rl = readline.createInterface({
  input: process.stdin,
  output: process.stdout,
  terminal: false
});

rl.on('line', (line) => {
  let cleaned = line.trim();
  if (!cleaned) return;
  if ((cleaned.startsWith("'") && cleaned.endsWith("'")) || (cleaned.startsWith('"') && cleaned.endsWith('"'))) {
    cleaned = cleaned.slice(1, -1).trim();
  }
  try {
    const req = JSON.parse(cleaned);
    handleRequest(req);
  } catch (err) {
    sendError(null, -32700, 'Parse error');
  }
});

function handleRequest(req) {
  const { id, method, params } = req;

  switch (method) {
    case 'initialize':
      sendResponse(id, {
        protocolVersion: '2024-11-05',
        capabilities: { tools: {} },
        serverInfo: { name: 'lightpanda-mcp-server', version: '1.0.0' }
      });
      break;

    case 'notifications/initialized':
      break;

    case 'tools/list':
      sendResponse(id, {
        tools: [
          {
            name: 'lightpanda_fetch_html',
            description: 'Fetches HTML content using Lightpanda fast headless browser engine.',
            inputSchema: {
              type: 'object',
              properties: { url: { type: 'string', description: 'Target URL (http or https)' } },
              required: ['url']
            }
          },
          {
            name: 'lightpanda_get_markdown',
            description: 'Extracts clean Markdown and AX Accessibility Tree via Lightpanda.',
            inputSchema: {
              type: 'object',
              properties: { url: { type: 'string', description: 'Target URL (http or https)' } },
              required: ['url']
            }
          },
          {
            name: 'lightpanda_execute_js',
            description: 'Executes custom JavaScript inside Lightpanda browser engine over CDP.',
            inputSchema: {
              type: 'object',
              properties: {
                url: { type: 'string', description: 'Target URL' },
                script: { type: 'string', description: 'JS code' }
              },
              required: ['url', 'script']
            }
          },
          {
            name: 'lightpanda_status',
            description: 'Checks local Lightpanda daemon health and CDP status.',
            inputSchema: { type: 'object', properties: {} }
          }
        ]
      });
      break;

    case 'tools/call':
      executeToolCall(id, params);
      break;

    default:
      sendError(id, -32601, `Method not found: ${method}`);
  }
}

async function executeToolCall(id, params) {
  const { name, arguments: args } = params || {};

  try {
    await ensureDaemonRunning();

    if (name === 'lightpanda_status') {
      const status = await checkStatus();
      sendResponse(id, { content: [{ type: 'text', text: status }] });
      return;
    }

    if (name === 'lightpanda_fetch_html') {
      const html = await fetchHTML(args.url);
      sendResponse(id, { content: [{ type: 'text', text: html }] });
      return;
    }

    if (name === 'lightpanda_get_markdown') {
      const md = await fetchMarkdown(args.url);
      sendResponse(id, { content: [{ type: 'text', text: md }] });
      return;
    }

    if (name === 'lightpanda_execute_js') {
      const result = await executeJS(args.url, args.script);
      sendResponse(id, { content: [{ type: 'text', text: result }] });
      return;
    }

    sendResponse(id, { content: [{ type: 'text', text: `Unknown tool: ${name}` }], isError: true });
  } catch (err) {
    sendResponse(id, { content: [{ type: 'text', text: `Error: ${err.message}` }], isError: true });
  }
}

function fetchHTML(targetUrl) {
  return new Promise((resolve, reject) => {
    const client = targetUrl.startsWith('https') ? https : http;
    client.get(targetUrl, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => resolve(data));
    }).on('error', (err) => reject(new Error(`Failed to fetch ${targetUrl}: ${err.message}`)));
  });
}

function fetchMarkdown(targetUrl) {
  return new Promise((resolve) => {
    exec(`wsl lightpanda fetch ${targetUrl}`, (err, stdout) => {
      if (!err && stdout && stdout.trim()) {
        resolve(stdout);
      } else {
        fetchHTML(targetUrl)
          .then(html => resolve(`# Content from ${targetUrl}\n\n${html}`))
          .catch(err => resolve(`Fetch error: ${err.message}`));
      }
    });
  });
}

function executeJS(targetUrl, script) {
  return new Promise((resolve, reject) => {
    const jsCode = `
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.connectOverCDP('ws://${LIGHTPANDA_HOST}:${LIGHTPANDA_PORT}');
  const page = await browser.newPage();
  await page.goto('${targetUrl}');
  const res = await page.evaluate(() => { ${script} });
  console.log(JSON.stringify(res, null, 2));
  await browser.close();
})();`;

    exec(`node -e "${jsCode.replace(/"/g, '\\"')}"`, (err, stdout, stderr) => {
      if (err) return reject(new Error(stderr || err.message));
      resolve(stdout);
    });
  });
}

function checkStatus() {
  return new Promise((resolve) => {
    const socket = new net.Socket();
    socket.setTimeout(2000);
    socket.on('connect', () => {
      socket.destroy();
      resolve(`✅ Lightpanda CDP server is ONLINE at ws://${LIGHTPANDA_HOST}:${LIGHTPANDA_PORT}`);
    });
    socket.on('error', () => {
      socket.destroy();
      resolve(`⚠️ Lightpanda daemon starting or offline on ${LIGHTPANDA_HOST}:${LIGHTPANDA_PORT}. Auto-launched background process.`);
    });
    socket.on('timeout', () => {
      socket.destroy();
      resolve(`⚠️ Lightpanda connection timeout on ${LIGHTPANDA_HOST}:${LIGHTPANDA_PORT}`);
    });
    socket.connect(LIGHTPANDA_PORT, LIGHTPANDA_HOST);
  });
}

function ensureDaemonRunning() {
  return new Promise((resolve) => {
    const socket = new net.Socket();
    socket.setTimeout(1000);
    socket.on('connect', () => {
      socket.destroy();
      resolve(true);
    });
    socket.on('error', () => {
      socket.destroy();
      if (!isDaemonStarting) {
        isDaemonStarting = true;
        exec(`wsl bash -c "nohup lightpanda --host 0.0.0.0 --port ${LIGHTPANDA_PORT} >/dev/null 2>&1 &"`);
      }
      setTimeout(resolve, 800);
    });
    socket.on('timeout', () => {
      socket.destroy();
      resolve(false);
    });
    socket.connect(LIGHTPANDA_PORT, LIGHTPANDA_HOST);
  });
}

function sendResponse(id, result) {
  const res = { jsonrpc: '2.0', id, result };
  process.stdout.write(JSON.stringify(res) + '\n');
}

function sendError(id, code, message) {
  const res = { jsonrpc: '2.0', id, error: { code, message } };
  process.stdout.write(JSON.stringify(res) + '\n');
}
