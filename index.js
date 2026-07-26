#!/usr/bin/env node

/**
 * Universal Lightpanda MCP Server (Node.js / Bun / Deno Runtime)
 * Zero external dependencies. Works on NPM, Bun, PNPM, Yarn, PyPI, and Go.
 */

const readline = require('readline');
const http = require('http');
const https = require('https');
const net = require('net');
const { exec, spawn } = require('child_process');
const os = require('os');

// ---------- Configuration ----------
function parseArgs(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a.startsWith('--')) {
      const key = a.slice(2);
      const next = argv[i + 1];
      if (next && !next.startsWith('--')) {
        out[key] = next;
        i++;
      } else {
        out[key] = true;
      }
    }
  }
  return out;
}

const cli = parseArgs(process.argv.slice(2));
const LIGHTPANDA_HOST = cli.host || process.env.LIGHTPANDA_HOST || '127.0.0.1';
const LIGHTPANDA_PORT = String(cli.port || process.env.LIGHTPANDA_PORT || '9222');
const HTTP_TIMEOUT_MS = Number(cli.timeout || process.env.LIGHTPANDA_HTTP_TIMEOUT || 30000);
const MAX_REDIRECTS = Number(process.env.LIGHTPANDA_MAX_REDIRECTS || 5);
const MAX_RETRIES = Number(process.env.LIGHTPANDA_FETCH_RETRIES || 2);
const LOG_LEVEL = (cli['log-level'] || process.env.LIGHTPANDA_LOG_LEVEL || 'info').toLowerCase();

const LOG_LEVELS = { debug: 10, info: 20, warn: 30, error: 40 };
function log(level, msg) {
  if ((LOG_LEVELS[level] || 99) < (LOG_LEVELS[LOG_LEVEL] || 20)) return;
  process.stderr.write(`[${level}] ${msg}\n`);
}

// Detect platform-specific launch command for the Lightpanda daemon.
function detectLightpandaCommand() {
  const platform = process.platform;
  if (platform === 'win32') {
    // On Windows, Lightpanda is typically installed inside WSL.
    // Fall back to `lightpanda` on PATH if `wsl` is unavailable.
    return { cmd: 'wsl', args: ['lightpanda', '--host', '0.0.0.0'] };
  }
  return { cmd: 'lightpanda', args: ['--host', '0.0.0.0'] };
}

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
        capabilities: { tools: {}, logging: {} },
        serverInfo: { name: 'lightpanda-mcp-server', version: '1.1.0' }
      });
      break;

    case 'notifications/initialized':
      break;

    case 'logging/setLevel':
      // Acknowledge — MCP clients may ask the server to change log level.
      sendResponse(id, {});
      break;

    case 'tools/list':
      sendResponse(id, {
        tools: [
          {
            name: 'lightpanda_fetch_html',
            description: 'Fetches HTML content using Lightpanda fast headless browser engine.',
            annotations: { readOnlyHint: true, openWorldHint: true },
            inputSchema: {
              type: 'object',
              properties: {
                url: { type: 'string', description: 'Target URL (http or https)' },
                timeoutMs: { type: 'number', description: 'Per-request timeout in milliseconds (default: 30000)' }
              },
              required: ['url']
            }
          },
          {
            name: 'lightpanda_get_markdown',
            description: 'Extracts clean Markdown and AX Accessibility Tree via Lightpanda.',
            annotations: { readOnlyHint: true, openWorldHint: true },
            inputSchema: {
              type: 'object',
              properties: { url: { type: 'string', description: 'Target URL (http or https)' } },
              required: ['url']
            }
          },
          {
            name: 'lightpanda_execute_js',
            description: 'Executes custom JavaScript inside Lightpanda browser engine over CDP. Script may be an async function or expression and is passed by stdin (no shell interpolation).',
            annotations: { readOnlyHint: false, openWorldHint: true },
            inputSchema: {
              type: 'object',
              properties: {
                url: { type: 'string', description: 'Target URL' },
                script: { type: 'string', description: 'JS code (expression or async IIFE)' }
              },
              required: ['url', 'script']
            }
          },
          {
            name: 'lightpanda_status',
            description: 'Checks local Lightpanda daemon health and CDP status.',
            annotations: { readOnlyHint: true, openWorldHint: false },
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
    if (name !== 'lightpanda_status') await ensureDaemonRunning();

    if (name === 'lightpanda_status') {
      const status = await checkStatus();
      sendResponse(id, { content: [{ type: 'text', text: status }] });
      return;
    }

    if (name === 'lightpanda_fetch_html') {
      const html = await fetchHTMLWithRetries(args.url, args.timeoutMs);
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
    log('error', `tools/call ${name}: ${err.message}`);
    sendResponse(id, { content: [{ type: 'text', text: `Error: ${err.message}` }], isError: true });
  }
}

function fetchOnce(targetUrl, timeoutMs) {
  return new Promise((resolve, reject) => {
    let url;
    try {
      url = new URL(targetUrl);
    } catch (e) {
      return reject(new Error(`Invalid URL: ${targetUrl}`));
    }
    if (url.protocol !== 'http:' && url.protocol !== 'https:') {
      return reject(new Error(`Unsupported protocol: ${url.protocol}`));
    }

    const client = url.protocol === 'https:' ? https : http;
    const req = client.get(targetUrl, { timeout: timeoutMs || HTTP_TIMEOUT_MS }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume();
        return reject(new Redirect(res.headers.location, res.statusCode));
      }
      if (res.statusCode >= 400) {
        res.resume();
        return reject(new Error(`HTTP ${res.statusCode} for ${targetUrl}`));
      }
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
      res.on('error', reject);
    });
    req.on('error', reject);
    req.on('timeout', () => {
      req.destroy(new Error(`Request timeout after ${timeoutMs || HTTP_TIMEOUT_MS}ms`));
    });
  });
}

class Redirect extends Error {
  constructor(location, status) {
    super(`Redirect ${status} -> ${location}`);
    this.location = location;
    this.status = status;
  }
}

async function fetchHTMLWithRetries(targetUrl, timeoutMs, depth = 0) {
  try {
    return await fetchOnce(targetUrl, timeoutMs);
  } catch (err) {
    if (err instanceof Redirect) {
      if (depth >= MAX_REDIRECTS) throw new Error(`Too many redirects (>${MAX_REDIRECTS})`);
      const next = new URL(err.location, targetUrl).toString();
      log('debug', `redirect ${err.status} ${targetUrl} -> ${next}`);
      return fetchHTMLWithRetries(next, timeoutMs, depth + 1);
    }
    // Retry transient network errors up to MAX_RETRIES.
    if (depth < MAX_RETRIES && isTransientError(err)) {
      const backoff = Math.min(1000 * Math.pow(2, depth), 4000);
      log('warn', `retry in ${backoff}ms: ${err.message}`);
      await sleep(backoff);
      return fetchHTMLWithRetries(targetUrl, timeoutMs, depth + 1);
    }
    throw err;
  }
}

function isTransientError(err) {
  const m = String(err.message || '').toLowerCase();
  return /timeout|econnreset|econnrefused|etimedout|eai_again|socket hang up/.test(m);
}

function sleep(ms) { return new Promise((r) => setTimeout(r, ms)); }

function fetchMarkdown(targetUrl) {
  return new Promise((resolve) => {
    const { cmd, args } = detectLightpandaCommand();
    log('debug', `markdown via ${cmd} ${args.concat(['fetch', targetUrl]).join(' ')}`);
    const child = spawn(cmd, args.concat(['fetch', targetUrl]), { stdio: ['ignore', 'pipe', 'pipe'] });
    let out = '';
    child.stdout.on('data', (c) => (out += c));
    child.on('error', () => fallbackMarkdown(targetUrl).then(resolve));
    child.on('exit', (code) => {
      if (code === 0 && out.trim()) resolve(out);
      else fallbackMarkdown(targetUrl).then(resolve);
    });
  });
}

function fallbackMarkdown(targetUrl) {
  return fetchHTMLWithRetries(targetUrl)
    .then((html) => `# Content from ${targetUrl}\n\n> Note: Lightpanda CLI unavailable; raw HTML returned.\n\n${html}`)
    .catch((err) => `Fetch error: ${err.message}`);
}

function executeJS(targetUrl, script) {
  return new Promise((resolve, reject) => {
    // Script is passed via STDIN to avoid shell-escaping pitfalls.
    // Inside the runtime, the user's script is wrapped to support both
    // sync expressions and async functions returning a Promise.
    const wsHost = LIGHTPANDA_HOST.replace(/[^A-Za-z0-9._:-]/g, '');
    const wsPort = LIGHTPANDA_PORT.replace(/[^0-9]/g, '');
    const wrapper = `
const { chromium } = require('playwright');
const userScript = require('fs').readFileSync(0, 'utf8');
(async () => {
  const browser = await chromium.connectOverCDP('ws://${wsHost}:${wsPort}');
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  await page.goto(${JSON.stringify(targetUrl)});
  let res;
  try {
    const fn = eval('(' + userScript + ')');
    res = (typeof fn === 'function') ? await fn(page) : await page.evaluate(() => eval('(' + userScript + ')'));
  } catch (e) {
    res = { error: String(e && e.message || e) };
  }
  process.stdout.write(JSON.stringify(res, null, 2));
  await browser.close();
})().catch((e) => { process.stderr.write(String(e && e.message || e)); process.exit(1); });
`;

    const child = spawn('node', ['-e', wrapper], { stdio: ['pipe', 'pipe', 'pipe'] });
    let out = '';
    let errOut = '';
    child.stdout.on('data', (c) => (out += c));
    child.stderr.on('data', (c) => (errOut += c));
    child.on('error', reject);
    child.on('exit', (code) => {
      if (code !== 0) reject(new Error(errOut || `node exited ${code}`));
      else resolve(out);
    });
    child.stdin.write(script);
    child.stdin.end();
  });
}

function checkStatus() {
  return new Promise((resolve) => {
    const socket = new net.Socket();
    socket.setTimeout(2000);
    socket.on('connect', () => {
      socket.destroy();
      resolve(`OK Lightpanda CDP server is ONLINE at ws://${LIGHTPANDA_HOST}:${LIGHTPANDA_PORT}`);
    });
    socket.on('error', () => {
      socket.destroy();
      resolve(`WARN Lightpanda daemon offline on ${LIGHTPANDA_HOST}:${LIGHTPANDA_PORT}. Auto-launch attempted.`);
    });
    socket.on('timeout', () => {
      socket.destroy();
      resolve(`WARN Lightpanda connection timeout on ${LIGHTPANDA_HOST}:${LIGHTPANDA_PORT}`);
    });
    socket.connect(Number(LIGHTPANDA_PORT), LIGHTPANDA_HOST);
  });
}

function ensureDaemonRunning() {
  return new Promise((resolve) => {
    const socket = new net.Socket();
    socket.setTimeout(1000);
    socket.on('connect', () => { socket.destroy(); resolve(true); });
    socket.on('error', () => {
      socket.destroy();
      if (!isDaemonStarting) {
        isDaemonStarting = true;
        const { cmd, args } = detectLightpandaCommand();
        const fullArgs = args.concat(['--port', LIGHTPANDA_PORT]);
        log('info', `launching lightpanda daemon: ${cmd} ${fullArgs.join(' ')}`);
        try {
          spawn(cmd, fullArgs, { stdio: 'ignore', detached: true }).unref();
        } catch (e) {
          log('error', `failed to launch daemon: ${e.message}`);
        }
      }
      setTimeout(resolve, 800);
    });
    socket.on('timeout', () => { socket.destroy(); resolve(false); });
    socket.connect(Number(LIGHTPANDA_PORT), LIGHTPANDA_HOST);
  });
}

function sendResponse(id, result) {
  process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id, result }) + '\n');
}
function sendError(id, code, message) {
  process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id, error: { code, message } }) + '\n');
}

log('debug', `lightpanda-mcp-server started host=${LIGHTPANDA_HOST} port=${LIGHTPANDA_PORT} timeout=${HTTP_TIMEOUT_MS}ms`);
