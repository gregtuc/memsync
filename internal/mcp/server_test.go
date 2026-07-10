package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func testTools() []Tool {
	return []Tool{
		{
			Name:        "echo",
			Description: "echoes its text argument",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
			Handler: func(a map[string]any) (string, error) {
				s, _ := a["text"].(string)
				return "you said: " + s, nil
			},
		},
	}
}

func run(t *testing.T, input string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, ServerInfo{Name: "memsync", Version: "test"}, testTools()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var msgs []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("response is not JSON: %q: %v", line, err)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func TestInitializeEchoesProtocolVersionAndAdvertisesTools(t *testing.T) {
	msgs := run(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`+"\n")
	if len(msgs) != 1 {
		t.Fatalf("want 1 response, got %d", len(msgs))
	}
	result := msgs[0]["result"].(map[string]any)
	if result["protocolVersion"] != "2025-03-26" {
		t.Fatalf("protocol version not echoed: %v", result["protocolVersion"])
	}
	caps := result["capabilities"].(map[string]any)
	if _, ok := caps["tools"]; !ok {
		t.Fatal("tools capability missing")
	}
	if result["serverInfo"].(map[string]any)["name"] != "memsync" {
		t.Fatalf("server name wrong: %v", result["serverInfo"])
	}
}

func TestInitializeDefaultsProtocolVersionWhenAbsent(t *testing.T) {
	msgs := run(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`+"\n")
	if v := msgs[0]["result"].(map[string]any)["protocolVersion"]; v != defaultProtocolVersion {
		t.Fatalf("want default protocol version, got %v", v)
	}
}

func TestToolsListReturnsSchemas(t *testing.T) {
	msgs := run(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n")
	tools := msgs[0]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("want 1 tool, got %d", len(tools))
	}
	first := tools[0].(map[string]any)
	if first["name"] != "echo" || first["inputSchema"] == nil {
		t.Fatalf("tool advertised wrong: %+v", first)
	}
}

func TestToolsCallInvokesHandler(t *testing.T) {
	msgs := run(t, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`+"\n")
	result := msgs[0]["result"].(map[string]any)
	if result["isError"].(bool) {
		t.Fatal("unexpected error result")
	}
	content := result["content"].([]any)[0].(map[string]any)
	if content["text"] != "you said: hi" {
		t.Fatalf("handler output wrong: %v", content["text"])
	}
}

func TestUnknownToolAndMethodAreReported(t *testing.T) {
	msgs := run(t,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nope","arguments":{}}}`+"\n"+
			`{"jsonrpc":"2.0","id":5,"method":"does/notExist"}`+"\n")
	if len(msgs) != 2 {
		t.Fatalf("want 2 responses, got %d", len(msgs))
	}
	if msgs[0]["error"] == nil {
		t.Fatal("unknown tool should be a JSON-RPC error")
	}
	if code := msgs[1]["error"].(map[string]any)["code"].(float64); code != -32601 {
		t.Fatalf("want method-not-found (-32601), got %v", code)
	}
}

func TestNotificationsGetNoResponse(t *testing.T) {
	// notifications/initialized has no id and must not produce a response.
	msgs := run(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")
	if len(msgs) != 0 {
		t.Fatalf("notification produced a response: %+v", msgs)
	}
}

func TestBlankLinesAndEOFAreClean(t *testing.T) {
	// Trailing content with no newline must still be processed.
	msgs := run(t, "\n\n"+`{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	if len(msgs) != 1 || msgs[0]["result"] == nil {
		t.Fatalf("ping without trailing newline not handled: %+v", msgs)
	}
}
