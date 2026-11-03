package distribution

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeClientConfigsDoNotEmbedCredentials(t *testing.T) {
	t.Setenv("ARIA_MERCHANT_TOKEN", "must-not-be-written")
	binary := filepath.Join(t.TempDir(), "folder with spaces", "aria-commerce-mcp")
	for _, client := range []string{"codex", "claude-code", "aria"} {
		data, err := ClientConfig(client, binary)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "must-not-be-written") || strings.Contains(string(data), "ARIA_MERCHANT_LOGIN_TOKEN") {
			t.Fatal("credential or human login authority included")
		}
		if client == "codex" {
			if !strings.Contains(string(data), "[mcp_servers.aria-commerce]") || !strings.Contains(string(data), "env_vars =") {
				t.Fatal(string(data))
			}
		} else {
			var doc map[string]json.RawMessage
			if json.Unmarshal(data, &doc) != nil {
				t.Fatal("invalid JSON")
			}
			key := "servers"
			if client == "claude-code" {
				key = "mcpServers"
			}
			if doc[key] == nil {
				t.Fatal("wrong client wrapper", string(data))
			}
		}
	}
	if _, err := ClientConfig("codex", "relative/binary"); err == nil {
		t.Fatal("relative binary path accepted")
	}
	if _, err := ClientConfig("unknown", binary); err == nil {
		t.Fatal("unknown client accepted")
	}
}
