package distribution

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
)

// ClientConfig renders a native configuration without reading or persisting secrets.
// Installation and importing this configuration are explicit user actions.
func ClientConfig(client, binary string) ([]byte, error) {
	if !filepath.IsAbs(binary) {
		return nil, errors.New("provide a literal absolute MCP binary path")
	}
	if client == "codex" {
		return []byte(fmt.Sprintf("[mcp_servers.aria-commerce]\ncommand = %s\nargs = []\nenv_vars = [\"ARIA_COMMERCE_URL\", \"ARIA_MERCHANT_TOKEN\", \"ARIA_CLOUD_RUN_AUDIENCE\"]\n", strconv.Quote(binary))), nil
	}
	entry := map[string]any{"command": binary, "args": []string{}, "env": map[string]string{"ARIA_COMMERCE_URL": "${ARIA_COMMERCE_URL}", "ARIA_MERCHANT_TOKEN": "${ARIA_MERCHANT_TOKEN}"}}
	var doc any
	switch client {
	case "claude-code":
		doc = map[string]any{"mcpServers": map[string]any{"aria-commerce": entry}}
	case "aria":
		entry["name"] = "aria-commerce"
		entry["enabled"] = true
		doc = map[string]any{"servers": []any{entry}}
	default:
		return nil, errors.New("client must be codex, claude-code or aria")
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	return append(data, '\n'), err
}
