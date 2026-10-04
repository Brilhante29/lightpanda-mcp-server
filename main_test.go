package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func captureOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := output
	buffer := &bytes.Buffer{}
	output = buffer
	t.Cleanup(func() { output = previous })
	return buffer
}

func decodeResponses(t *testing.T, raw string) []Response {
	t.Helper()
	var responses []Response
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var response Response
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("invalid JSON-RPC line %q: %v", line, err)
		}
		responses = append(responses, response)
	}
	return responses
}

func TestValidateURLAcceptsOnlyAbsoluteHTTPURLs(t *testing.T) {
	for _, valid := range []string{"https://example.com", "http://127.0.0.1:8080/path?q=1"} {
		if _, err := validateURL(valid); err != nil {
			t.Errorf("validateURL(%q) unexpected error: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"", "example.com", "file:///etc/passwd", "javascript:alert(1)",
		"https://", "--dump", "ftp://example.com",
	} {
		if _, err := validateURL(invalid); err == nil {
			t.Errorf("validateURL(%q) accepted an invalid URL", invalid)
		}
	}
}

func TestRenderArgsKeepTheURLAsOneArgument(t *testing.T) {
	target, err := validateURL("https://example.com/a?b=c;touch%20pwned")
	if err != nil {
		t.Fatal(err)
	}
	args := renderArgs(target)
	if len(args) != 3 || args[0] != "fetch" || args[1] != "--dump" || args[2] != target {
		t.Fatalf("unexpected arguments: %#v", args)
	}
}

func TestFetchHTMLReturnsBodyAndRejectsErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer server.Close()

	body, err := fetchHTML(server.URL)
	if err != nil || !strings.Contains(body, "ok") {
		t.Fatalf("fetchHTML() = %q, %v", body, err)
	}
	if _, err := fetchHTML(server.URL + "/missing"); err == nil {
		t.Fatal("fetchHTML accepted an HTTP 404")
	}
}

func TestToolsListAdvertisesOnlyImplementedTools(t *testing.T) {
	buffer := captureOutput(t)
	serve(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"))

	responses := decodeResponses(t, buffer.String())
	if len(responses) != 1 || responses[0].Error != nil {
		t.Fatalf("unexpected responses: %+v", responses)
	}
	encoded, _ := json.Marshal(responses[0].Result)
	for _, name := range []string{"fetch_html", "lightpanda_render_html", "lightpanda_status"} {
		if !strings.Contains(string(encoded), name) {
			t.Errorf("tools/list is missing %s", name)
		}
	}
	if strings.Contains(string(encoded), "execute_js") {
		t.Error("tools/list must not advertise JavaScript execution")
	}
}

func TestInvalidURLIsAToolErrorNotAProtocolError(t *testing.T) {
	buffer := captureOutput(t)
	serve(strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"fetch_html","arguments":{"url":"file:///etc/passwd"}}}` + "\n"))

	responses := decodeResponses(t, buffer.String())
	if len(responses) != 1 {
		t.Fatalf("expected one response, got %d", len(responses))
	}
	encoded, _ := json.Marshal(responses[0].Result)
	if !strings.Contains(string(encoded), `"isError":true`) {
		t.Fatalf("expected a tool error, got %s", encoded)
	}
}

func TestMalformedLinesAndUnknownMethods(t *testing.T) {
	buffer := captureOutput(t)
	serve(strings.NewReader("not json\n" + `{"jsonrpc":"2.0","id":3,"method":"nope"}` + "\n" + `'{"jsonrpc":"2.0","id":4,"method":"initialize"}'` + "\n"))

	responses := decodeResponses(t, buffer.String())
	if len(responses) != 3 {
		t.Fatalf("expected three responses, got %d", len(responses))
	}
	if responses[0].Error == nil || responses[0].Error.Code != -32700 {
		t.Errorf("expected a parse error, got %+v", responses[0])
	}
	if responses[1].Error == nil || responses[1].Error.Code != -32601 {
		t.Errorf("expected method not found, got %+v", responses[1])
	}
	if responses[2].Error != nil {
		t.Errorf("quoted initialize should succeed, got %+v", responses[2].Error)
	}
}
