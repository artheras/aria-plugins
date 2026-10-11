// commerce-mcp adapts the Commerce HTTP API to MCP stdio.
// It intentionally exposes no approval, checkout, or capture tools.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type bridge struct {
	base, token string
	client      *http.Client
}

func (b bridge) request(ctx context.Context, method, path string, body any) (string, error) {
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.base+path, reader)
	if err != nil {
		return "", errors.New("invalid commerce request")
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := b.client.Do(req)
	if err != nil {
		return "", errors.New("commerce service unavailable")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 256<<10))
	if err != nil {
		return "", errors.New("commerce response unavailable")
	}
	if !json.Valid(raw) {
		return "", errors.New("invalid commerce response")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var out struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &out)
		if out.Error == "" {
			out.Error = "commerce request rejected"
		}
		return "", errors.New(out.Error)
	}
	return string(raw), nil
}
func toolsList() []any {
	return []any{
		map[string]any{"name": "commerce_catalog", "description": "List trusted catalogue SKUs and integer-cent prices. Read-only.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
		map[string]any{"name": "commerce_draft_order", "description": "Create a draft from a request. The human must review and approve in the Commerce website. This tool cannot charge or approve.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"prompt": map[string]any{"type": "string", "minLength": 3, "maxLength": 2000}}, "required": []string{"prompt"}, "additionalProperties": false}},
		map[string]any{"name": "commerce_get_order", "description": "Read a merchant-owned order and its audit trail. Does not change payment state.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"order_id": map[string]any{"type": "string", "pattern": "^[a-f0-9]{32}$"}}, "required": []string{"order_id"}, "additionalProperties": false}},
	}
}
func toolResult(text string, failed bool) any {
	return map[string]any{"content": []any{map[string]string{"type": "text", "text": text}}, "isError": failed}
}
func (b bridge) call(params json.RawMessage) any {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(params, &call) != nil {
		return toolResult("invalid tool request", true)
	}
	var args struct {
		Prompt  string `json:"prompt,omitempty"`
		OrderID string `json:"order_id,omitempty"`
	}
	if len(call.Arguments) == 0 {
		call.Arguments = json.RawMessage("{}")
	}
	d := json.NewDecoder(bytes.NewReader(call.Arguments))
	d.DisallowUnknownFields()
	if d.Decode(&args) != nil {
		return toolResult("invalid tool arguments", true)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	method, path, body := "GET", "/api/catalog", any(nil)
	switch call.Name {
	case "commerce_catalog":
		if args.Prompt != "" || args.OrderID != "" {
			return toolResult("catalog accepts no arguments", true)
		}
	case "commerce_draft_order":
		if len(args.Prompt) < 3 || len(args.Prompt) > 2000 || args.OrderID != "" {
			return toolResult("provide a prompt of 3 to 2000 characters", true)
		}
		method = "POST"
		path = "/api/orders"
		body = map[string]string{"prompt": args.Prompt}
	case "commerce_get_order":
		if len(args.OrderID) != 32 || strings.Trim(args.OrderID, "abcdef0123456789") != "" || args.Prompt != "" {
			return toolResult("provide a valid order_id", true)
		}
		path = "/api/orders/" + args.OrderID
	default:
		return toolResult("unknown tool; payment actions are only available to the human in the browser", true)
	}
	out, err := b.request(ctx, method, path, body)
	if err != nil {
		return toolResult(err.Error(), true)
	}
	if call.Name == "commerce_draft_order" {
		out += "\nReview in the merchant browser session at " + b.base + "/. No approval or payment has been made."
	}
	return toolResult(out, false)
}
func (b bridge) result(req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			Protocol string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.Protocol != "2024-11-05" && p.Protocol != "2025-06-18" {
			p.Protocol = "2025-06-18"
		}
		return map[string]any{"protocolVersion": p.Protocol, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "aria-commerce", "version": "0.1.0"}}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolsList()}, nil
	case "tools/call":
		return b.call(req.Params), nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func serve(reader io.Reader, writer io.Writer, b bridge) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 256<<10)
	encoder := json.NewEncoder(writer)
	for scanner.Scan() {
		var req rpcRequest
		if json.Unmarshal(scanner.Bytes(), &req) != nil || req.JSONRPC != "2.0" {
			if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": nil, "error": rpcError{Code: -32700, Message: "invalid JSON-RPC request"}}); err != nil {
				return err
			}
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		result, rpcErr := b.result(req)
		response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			response["error"] = rpcErr
		} else {
			response["result"] = result
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}
func main() {
	base := strings.TrimRight(os.Getenv("ARIA_COMMERCE_URL"), "/")
	if base == "" {
		base = "http://127.0.0.1:4180"
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		fmt.Fprintln(os.Stderr, "ARIA_COMMERCE_URL must be an HTTPS origin or HTTP localhost")
		os.Exit(1)
	}
	token := os.Getenv("ARIA_MERCHANT_TOKEN")
	if len(token) < 32 {
		fmt.Fprintln(os.Stderr, "configure ARIA_MERCHANT_TOKEN (at least 32 characters) on both Commerce and MCP")
		os.Exit(1)
	}
	b := bridge{base: base, token: token, client: &http.Client{Timeout: 55 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if err = serve(os.Stdin, os.Stdout, b); err != nil {
		fmt.Fprintln(os.Stderr, "MCP transport stopped")
		os.Exit(1)
	}
}
