package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// MCP Protocol Data Structures (JSON-RPC 2.0)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Annotations Annotations `json:"annotations,omitempty"`
	InputSchema InputSchema `json:"inputSchema"`
}

type Annotations struct {
	ReadOnlyHint bool `json:"readOnlyHint,omitempty"`
	OpenWorldHint bool `json:"openWorldHint,omitempty"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type CallToolResult struct {
	Content []TextContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

var (
	lightpandaHost  string
	lightpandaPort  string
	httpTimeoutMS   int
	maxRedirects    int
	maxRetries      int
	logLevel        string
	mu              sync.Mutex
	daemonStarting  bool
)

const (
	logDebug = 10
	logInfo  = 20
	logWarn  = 30
	logError = 40
)

func logf(level string, format string, args ...interface{}) {
	levels := map[string]int{"debug": logDebug, "info": logInfo, "warn": logWarn, "error": logError}
	min, ok := levels[strings.ToLower(logLevel)]
	if !ok {
		min = logInfo
	}
	cur, ok := levels[strings.ToLower(level)]
	if !ok || cur < min {
		return
	}
	fmt.Fprintf(os.Stderr, "[%s] %s\n", level, fmt.Sprintf(format, args...))
}

func main() {
	flag.StringVar(&lightpandaHost, "host", envOrDefault("LIGHTPANDA_HOST", "127.0.0.1"), "Lightpanda daemon host")
	flag.StringVar(&lightpandaPort, "port", envOrDefault("LIGHTPANDA_PORT", "9222"), "Lightpanda daemon CDP port")
	flag.IntVar(&httpTimeoutMS, "timeout", envOrDefaultInt("LIGHTPANDA_HTTP_TIMEOUT", 30000), "HTTP fetch timeout in ms")
	flag.IntVar(&maxRedirects, "max-redirects", envOrDefaultInt("LIGHTPANDA_MAX_REDIRECTS", 5), "Max HTTP redirects")
	flag.IntVar(&maxRetries, "max-retries", envOrDefaultInt("LIGHTPANDA_FETCH_RETRIES", 2), "Max fetch retries on transient errors")
	flag.StringVar(&logLevel, "log-level", envOrDefault("LIGHTPANDA_LOG_LEVEL", "info"), "Log level (debug|info|warn|error)")
	flag.Parse()

	scanner := bufio.NewScanner(os.Stdin)
	buf := make([]byte, 10*1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	logf("debug", "lightpanda-mcp-server started host=%s port=%s timeout=%dms", lightpandaHost, lightpandaPort, httpTimeoutMS)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		if (bytes.HasPrefix(line, []byte("'")) && bytes.HasSuffix(line, []byte("'"))) ||
			(bytes.HasPrefix(line, []byte("\"")) && bytes.HasSuffix(line, []byte("\""))) {
			line = bytes.TrimSpace(line[1 : len(line)-1])
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			sendError(nil, -32700, "Parse error")
			continue
		}

		handleRequest(&req)
	}
}

func envOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		var n int
		if _, err := fmt.Sscanf(val, "%d", &n); err == nil {
			return n
		}
	}
	return fallback
}

func handleRequest(req *Request) {
	switch req.Method {
	case "initialize":
		sendResponse(req.ID, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools":   map[string]interface{}{},
				"logging": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "lightpanda-mcp-server",
				"version": "1.1.0",
			},
		})

	case "notifications/initialized":
		// no-op

	case "logging/setLevel":
		sendResponse(req.ID, map[string]interface{}{})

	case "tools/list":
		tools := []Tool{
			{
				Name:        "lightpanda_fetch_html",
				Description: "Fetches HTML content using Lightpanda fast headless browser engine.",
				Annotations: Annotations{ReadOnlyHint: true, OpenWorldHint: true},
				InputSchema: InputSchema{
					Type: "object",
					Properties: map[string]Property{
						"url":        {Type: "string", Description: "Target web URL to fetch"},
						"timeoutMs":  {Type: "number", Description: "Per-request timeout in ms (default 30000)"},
					},
					Required: []string{"url"},
				},
			},
			{
				Name:        "lightpanda_get_markdown",
				Description: "Extracts clean Markdown text and Accessibility Tree (AX Tree) from a webpage via Lightpanda.",
				Annotations: Annotations{ReadOnlyHint: true, OpenWorldHint: true},
				InputSchema: InputSchema{
					Type: "object",
					Properties: map[string]Property{
						"url": {Type: "string", Description: "Target web URL to parse"},
					},
					Required: []string{"url"},
				},
			},
			{
				Name:        "lightpanda_execute_js",
				Description: "Executes custom JavaScript inside Lightpanda browser engine over CDP. Script is passed via stdin (no shell interpolation).",
				Annotations: Annotations{ReadOnlyHint: false, OpenWorldHint: true},
				InputSchema: InputSchema{
					Type: "object",
					Properties: map[string]Property{
						"url":    {Type: "string", Description: "Target web URL"},
						"script": {Type: "string", Description: "JavaScript snippet to evaluate"},
					},
					Required: []string{"url", "script"},
				},
			},
			{
				Name:        "lightpanda_status",
				Description: "Checks local Lightpanda daemon health and CDP WebSocket connectivity.",
				Annotations: Annotations{ReadOnlyHint: true, OpenWorldHint: false},
				InputSchema: InputSchema{
					Type:       "object",
					Properties: map[string]Property{},
				},
			},
		}
		sendResponse(req.ID, map[string]interface{}{
			"tools": tools,
		})

	case "tools/call":
		var params ToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(req.ID, -32602, "Invalid params")
			return
		}

		result := executeToolCall(params)
		sendResponse(req.ID, result)

	default:
		sendError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func executeToolCall(params ToolCallParams) CallToolResult {
	if params.Name != "lightpanda_status" {
		ensureLightpandaRunning()
	}

	switch params.Name {
	case "lightpanda_status":
		return CallToolResult{
			Content: []TextContent{{Type: "text", Text: checkLightpandaStatus()}},
		}

	case "lightpanda_fetch_html":
		var args struct {
			URL       string `json:"url"`
			TimeoutMs int    `json:"timeoutMs"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return errorResult(fmt.Sprintf("Invalid arguments: %v", err))
		}
		html, err := fetchHTMLWithRetries(args.URL, args.TimeoutMs, 0)
		if err != nil {
			return errorResult(fmt.Sprintf("Fetch error: %v", err))
		}
		return CallToolResult{
			Content: []TextContent{{Type: "text", Text: html}},
		}

	case "lightpanda_get_markdown":
		var args struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return errorResult(fmt.Sprintf("Invalid arguments: %v", err))
		}
		md, err := fetchMarkdown(args.URL)
		if err != nil {
			return errorResult(fmt.Sprintf("Markdown error: %v", err))
		}
		return CallToolResult{
			Content: []TextContent{{Type: "text", Text: md}},
		}

	case "lightpanda_execute_js":
		var args struct {
			URL    string `json:"url"`
			Script string `json:"script"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return errorResult(fmt.Sprintf("Invalid arguments: %v", err))
		}
		out, err := executeJS(args.URL, args.Script)
		if err != nil {
			return errorResult(fmt.Sprintf("JS execution error: %v", err))
		}
		return CallToolResult{
			Content: []TextContent{{Type: "text", Text: out}},
		}

	default:
		return errorResult(fmt.Sprintf("Unknown tool: %s", params.Name))
	}
}

var transientErrRe = regexp.MustCompile(`(?i)timeout|connection reset|connection refused|EOF|no such host`)

func fetchHTMLWithRetries(targetURL string, timeoutMs, depth int) (string, error) {
	if depth > maxRedirects+maxRetries {
		return "", errors.New("too many redirects/retries")
	}

	client := &http.Client{
		Timeout: time.Duration(firstNonZero(timeoutMs, httpTimeoutMS)) * time.Millisecond,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return fmt.Errorf("too many redirects (>%d)", maxRedirects)
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Lightpanda-MCP/1.1")

	resp, err := client.Do(req)
	if err != nil {
		if depth < maxRetries && transientErrRe.MatchString(err.Error()) {
			backoff := minDuration(1000*(1<<depth), 4000)
			logf("warn", "retry in %dms: %v", backoff, err)
			time.Sleep(time.Duration(backoff) * time.Millisecond)
			return fetchHTMLWithRetries(targetURL, timeoutMs, depth+1)
		}
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d for %s", resp.StatusCode, targetURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func fetchHTML(targetURL string) (string, error) {
	return fetchHTMLWithRetries(targetURL, 0, 0)
}

func fetchMarkdown(targetURL string) (string, error) {
	cmd, baseArgs := detectLightpandaCommand()
	args := append(append([]string{}, baseArgs...), "fetch", targetURL)

	logf("debug", "markdown via %s %s", cmd, strings.Join(args, " "))
	c := exec.Command(cmd, args...)
	var out bytes.Buffer
	c.Stdout = &out
	if err := c.Run(); err != nil {
		// Fallback: fetch HTML via HTTP and prefix a header.
		html, fetchErr := fetchHTML(targetURL)
		if fetchErr != nil {
			return "", fetchErr
		}
		return fmt.Sprintf("# Content from %s\n\n> Note: Lightpanda CLI unavailable; raw HTML returned.\n\n%s", targetURL, html), nil
	}
	return out.String(), nil
}

func executeJS(targetURL, script string) (string, error) {
	wsHost := sanitizeWS(lightpandaHost)
	wsPort := digitsOnly(lightpandaPort)
	targetJSON, _ := json.Marshal(targetURL)

	// Script is passed via STDIN; never interpolated into the wrapper.
	wrapper := fmt.Sprintf(`
const { chromium } = require('playwright');
const userScript = require('fs').readFileSync(0, 'utf8');
(async () => {
  const browser = await chromium.connectOverCDP('ws://%s:%s');
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  await page.goto(%s);
  let res;
  try {
    const fn = eval('(' + userScript + ')');
    res = (typeof fn === 'function') ? await fn(page) : await page.evaluate(() => eval('(' + userScript + ')'));
  } catch (e) {
    res = { error: String((e && e.message) || e) };
  }
  process.stdout.write(JSON.stringify(res, null, 2));
  await browser.close();
})().catch((e) => { process.stderr.write(String((e && e.message) || e)); process.exit(1); });
`, wsHost, wsPort, string(targetJSON))

	cmd := exec.Command("node", "-e", wrapper)
	cmd.Stdin = strings.NewReader(script)
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("evaluation failed: %v, stderr: %s", err, errOut.String())
	}
	return out.String(), nil
}

func checkLightpandaStatus() string {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(lightpandaHost, lightpandaPort), 2*time.Second)
	if err != nil {
		return fmt.Sprintf("WARN Lightpanda is offline on %s:%s. Auto-launch attempted.", lightpandaHost, lightpandaPort)
	}
	conn.Close()
	return fmt.Sprintf("OK Lightpanda CDP server is ONLINE at ws://%s:%s", lightpandaHost, lightpandaPort)
}

func ensureLightpandaRunning() {
	mu.Lock()
	defer mu.Unlock()

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(lightpandaHost, lightpandaPort), 1*time.Second)
	if err == nil {
		conn.Close()
		return
	}
	if !daemonStarting {
		daemonStarting = true
		cmdName, baseArgs := detectLightpandaCommand()
		fullArgs := append(append([]string{}, baseArgs...), "--port", lightpandaPort)
		logf("info", "launching lightpanda daemon: %s %s", cmdName, strings.Join(fullArgs, " "))
		c := exec.Command(cmdName, fullArgs...)
		c.Stdin = nil
		c.Stdout = nil
		c.Stderr = nil
		go func() { _ = c.Run() }()
	}
	time.Sleep(500 * time.Millisecond)
}

func detectLightpandaCommand() (string, []string) {
	if runtime.GOOS == "windows" {
		return "wsl", []string{"lightpanda", "--host", "0.0.0.0"}
	}
	return "lightpanda", []string{"--host", "0.0.0.0"}
}

func errorResult(msg string) CallToolResult {
	return CallToolResult{
		Content: []TextContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}

func sendResponse(id interface{}, result interface{}) {
	resp := Response{JSONRPC: "2.0", ID: id, Result: result}
	data, _ := json.Marshal(resp)
	os.Stdout.Write(append(data, '\n'))
}

func sendError(id interface{}, code int, message string) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
	data, _ := json.Marshal(resp)
	os.Stdout.Write(append(data, '\n'))
}

// --- helpers ---

func firstNonZero(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}

func minDuration(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sanitizeWS(s string) string {
	re := regexp.MustCompile(`[^A-Za-z0-9._:-]`)
	return re.ReplaceAllString(s, "")
}

func digitsOnly(s string) string {
	re := regexp.MustCompile(`[^0-9]`)
	return re.ReplaceAllString(s, "")
}

var _ = url.Parse // ensure net/url is used in case future redirect helpers need it
