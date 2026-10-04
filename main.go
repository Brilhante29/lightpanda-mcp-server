// Command lightpanda-mcp-server is a Model Context Protocol (MCP) server that
// exposes page fetching and Lightpanda headless rendering to AI agents over
// stdio (JSON-RPC 2.0, one message per line).
//
// Untrusted input never reaches a shell or an interpreter: URLs are validated
// and passed to the Lightpanda binary as a single argument.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"time"
)

const (
	protocolVersion = "2024-11-05"
	serverVersion   = "1.1.0"

	maxMessageBytes  = 10 << 20
	maxResponseBytes = 10 << 20
	fetchTimeout     = 15 * time.Second
	renderTimeout    = 30 * time.Second
)

// JSON-RPC 2.0 and MCP data structures.

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
	InputSchema InputSchema `json:"inputSchema"`
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
	lightpandaHost = getEnvOrDefault("LIGHTPANDA_HOST", "127.0.0.1")
	lightpandaPort = getEnvOrDefault("LIGHTPANDA_PORT", "9222")
	lightpandaBin  = getEnvOrDefault("LIGHTPANDA_BIN", "lightpanda")

	// output is where responses are written; tests replace it.
	output io.Writer = os.Stdout
)

func getEnvOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func main() {
	serve(os.Stdin)
}

func serve(input io.Reader) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), maxMessageBytes)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		// Some clients wrap each message in quotes; tolerate that.
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

func tools() []Tool {
	urlProperty := map[string]Property{
		"url": {Type: "string", Description: "Absolute http or https URL"},
	}
	return []Tool{
		{
			Name:        "fetch_html",
			Description: "Fetches the raw HTML of a URL over HTTP. JavaScript is not executed.",
			InputSchema: InputSchema{Type: "object", Properties: urlProperty, Required: []string{"url"}},
		},
		{
			Name:        "lightpanda_render_html",
			Description: "Renders a URL with the Lightpanda headless browser (JavaScript executed) and returns the resulting HTML.",
			InputSchema: InputSchema{Type: "object", Properties: urlProperty, Required: []string{"url"}},
		},
		{
			Name:        "lightpanda_status",
			Description: "Checks whether a Lightpanda CDP server is reachable at LIGHTPANDA_HOST:LIGHTPANDA_PORT.",
			InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}},
		},
	}
}

func handleRequest(req *Request) {
	switch req.Method {
	case "initialize":
		sendResponse(req.ID, map[string]interface{}{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": "lightpanda-mcp-server", "version": serverVersion},
		})
	case "notifications/initialized":
		// Notifications receive no response.
	case "tools/list":
		sendResponse(req.ID, map[string]interface{}{"tools": tools()})
	case "tools/call":
		var params ToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(req.ID, -32602, "Invalid params")
			return
		}
		sendResponse(req.ID, executeToolCall(params))
	default:
		sendError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func executeToolCall(params ToolCallParams) CallToolResult {
	switch params.Name {
	case "lightpanda_status":
		return textResult(checkLightpandaStatus())
	case "fetch_html", "lightpanda_render_html":
		var args struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return errorResult(fmt.Sprintf("Invalid arguments: %v", err))
		}
		target, err := validateURL(args.URL)
		if err != nil {
			return errorResult(err.Error())
		}
		var body string
		if params.Name == "fetch_html" {
			body, err = fetchHTML(target)
		} else {
			body, err = renderHTML(target)
		}
		if err != nil {
			return errorResult(err.Error())
		}
		return textResult(body)
	default:
		return errorResult(fmt.Sprintf("Unknown tool: %s", params.Name))
	}
}

// validateURL accepts only absolute http(s) URLs with a host, so that file://,
// javascript: or option-like values never reach the network or a subprocess.
func validateURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %v", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("invalid URL: only http and https are allowed")
	}
	if parsed.Host == "" {
		return "", errors.New("invalid URL: host is required")
	}
	return parsed.String(), nil
}

func fetchHTML(target string) (string, error) {
	client := &http.Client{Timeout: fetchTimeout}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "lightpanda-mcp-server/"+serverVersion)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("fetch failed: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("fetch failed: %v", err)
	}
	if len(body) > maxResponseBytes {
		return "", fmt.Errorf("fetch failed: response larger than %d bytes", maxResponseBytes)
	}
	return string(body), nil
}

// renderArgs builds the Lightpanda invocation without a shell. The URL is a
// single argument and, being a validated http(s) URL, cannot look like an option.
func renderArgs(target string) []string {
	return []string{"fetch", "--dump", target}
}

func renderHTML(target string) (string, error) {
	if _, err := exec.LookPath(lightpandaBin); err != nil {
		return "", fmt.Errorf("Lightpanda binary %q not found: install it from https://lightpanda.io or set LIGHTPANDA_BIN", lightpandaBin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), renderTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, lightpandaBin, renderArgs(target)...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("render failed: %v: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	if stdout.Len() > maxResponseBytes {
		return "", fmt.Errorf("render failed: output larger than %d bytes", maxResponseBytes)
	}
	return stdout.String(), nil
}

func checkLightpandaStatus() string {
	address := net.JoinHostPort(lightpandaHost, lightpandaPort)
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return fmt.Sprintf("Lightpanda CDP server is not reachable at %s. Start it bound to localhost, for example: %s serve --host 127.0.0.1 --port %s", address, lightpandaBin, lightpandaPort)
	}
	conn.Close()
	return fmt.Sprintf("Lightpanda CDP server is reachable at ws://%s", address)
}

func textResult(text string) CallToolResult {
	return CallToolResult{Content: []TextContent{{Type: "text", Text: text}}}
}

func errorResult(msg string) CallToolResult {
	return CallToolResult{Content: []TextContent{{Type: "text", Text: msg}}, IsError: true}
}

func sendResponse(id interface{}, result interface{}) {
	writeMessage(Response{JSONRPC: "2.0", ID: id, Result: result})
}

func sendError(id interface{}, code int, message string) {
	writeMessage(Response{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: message}})
}

func writeMessage(resp Response) {
	data, err := json.Marshal(resp)
	if err != nil {
		data, _ = json.Marshal(Response{JSONRPC: "2.0", ID: resp.ID, Error: &RPCError{Code: -32603, Message: "Internal error"}})
	}
	output.Write(append(data, '\n'))
}
