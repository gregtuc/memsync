// Package mcp implements the server half of the Model Context Protocol over
// stdio (newline-delimited JSON-RPC 2.0) with no third-party dependencies. It
// supports the initialize handshake and tool listing/calling, which is all a
// read-only tool provider needs.
package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// defaultProtocolVersion is used only when the client does not name one.
const defaultProtocolVersion = "2025-06-18"

// Tool is a callable exposed to the model. Handler receives the decoded
// arguments object and returns text content, or an error that is surfaced to the
// model as an in-band tool error rather than a protocol-level failure.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     func(arguments map[string]any) (string, error)
}

// ServerInfo identifies the server in the initialize response.
type ServerInfo struct {
	Name    string
	Version string
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve runs the stdio message loop until in reaches EOF. Client-side problems
// become JSON-RPC error responses; a non-nil error is returned only when writing
// to out fails.
func Serve(in io.Reader, out io.Writer, info ServerInfo, tools []Tool) error {
	byName := make(map[string]Tool, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
	}
	reader := bufio.NewReader(in)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			if err := handleLine(out, info, tools, byName, line); err != nil {
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

func handleLine(out io.Writer, info ServerInfo, tools []Tool, byName map[string]Tool, line []byte) error {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return nil // malformed input with no recoverable id: ignore per JSON-RPC
	}
	result, rerr := dispatch(info, tools, byName, req)
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return nil // notification: no response
	}
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	if rerr != nil {
		resp.Error = rerr
	} else {
		resp.Result = result
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = out.Write(append(b, '\n'))
	return err
}

func dispatch(info ServerInfo, tools []Tool, byName map[string]Tool, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		version := defaultProtocolVersion
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if len(req.Params) > 0 && json.Unmarshal(req.Params, &p) == nil && p.ProtocolVersion != "" {
			version = p.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": info.Name, "version": info.Version},
		}, nil
	case "tools/list":
		list := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			schema := t.InputSchema
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			list = append(list, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": schema,
			})
		}
		return map[string]any{"tools": list}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return nil, &rpcError{Code: -32602, Message: "invalid params"}
			}
		}
		tool, ok := byName[p.Name]
		if !ok {
			return nil, &rpcError{Code: -32602, Message: "unknown tool: " + p.Name}
		}
		text, err := tool.Handler(p.Arguments)
		if err != nil {
			return toolResult(err.Error(), true), nil
		}
		return toolResult(text, false), nil
	case "ping":
		return map[string]any{}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}
