package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPStdioCompatibilityAndHumanOnlyActions(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"commerce_approve_order","arguments":{}}}
`
	var output bytes.Buffer
	if err := serve(strings.NewReader(input), &output, bridge{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatal("notification produced a reply or stdout log", output.String())
	}
	var init struct {
		Result struct {
			Protocol string `json:"protocolVersion"`
		} `json:"result"`
	}
	if json.Unmarshal([]byte(lines[0]), &init) != nil || init.Result.Protocol != "2024-11-05" {
		t.Fatal("existing Aria protocol not supported")
	}
	var tools struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	json.Unmarshal([]byte(lines[1]), &tools)
	if len(tools.Result.Tools) != 3 {
		t.Fatal("unexpected tools")
	}
	for _, tool := range tools.Result.Tools {
		if strings.Contains(tool.Name, "approve") || strings.Contains(tool.Name, "capture") || strings.Contains(tool.Name, "checkout") {
			t.Fatal("financial action exposed to agent")
		}
	}
	if !strings.Contains(lines[2], `"isError":true`) {
		t.Fatal("unknown financial tool did not fail")
	}
}
