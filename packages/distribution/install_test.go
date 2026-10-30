package distribution

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func testBundle(t *testing.T, extra string) (string, string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	z := zip.NewWriter(&buffer)
	manifest, _ := json.Marshal(Manifest{Name: "commerce", Version: "0.4.0", ContractVersion: "1", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH})
	files := map[string][]byte{"bundle.json": manifest, "bin/aria-commerce-mcp" + Extension(runtime.GOOS): []byte("binary fixture")}
	for _, name := range []string{".claude-plugin/marketplace.json", ".claude-plugin/skills.lock.json", "plugins/commerce/.aria-plugin/plugin.json", "plugins/commerce/.claude-plugin/plugin.json", "plugins/commerce/README.md", "plugins/commerce/commands/commerce.md", "plugins/commerce/skills/commerce/SKILL.md", "plugins/commerce/skills/commerce/skill-policy.json", "skills/commerce/SKILL.md", "skills/commerce/skill-policy.json", "plugins/finance/.aria-plugin/plugin.json", "plugins/finance/.claude-plugin/plugin.json", "plugins/finance/README.md", "plugins/finance/commands/finance.md", "plugins/finance/skills/finance/SKILL.md", "plugins/finance/skills/finance/skill-policy.json", "skills/finance/SKILL.md", "skills/finance/skill-policy.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = data
	}
	if extra != "" {
		files[extra] = []byte("escape")
	}
	for name, data := range files {
		entry, _ := z.Create(name)
		entry.Write(data)
	}
	z.Close()
	path := filepath.Join(root, "fixture.zip")
	os.WriteFile(path, buffer.Bytes(), 0600)
	h := sha256.Sum256(buffer.Bytes())
	return path, hex.EncodeToString(h[:]), root
}
func TestInstallChecksumAndPathBoundaries(t *testing.T) {
	path, sum, root := testBundle(t, "")
	if _, err := Install(path, string(bytes.Repeat([]byte("a"), 64)), filepath.Join(root, "install")); err == nil {
		t.Fatal("wrong checksum accepted")
	}
	i, err := Install(path, sum, filepath.Join(root, "install"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(i.Directory, "skills", "finance", "SKILL.md")); err != nil {
		t.Fatal("finance workflow missing from installed multi-plugin catalogue", err)
	}
	if _, err = ReadInstallation(i.Directory); err != nil {
		t.Fatal(err)
	}
	if _, err = Install(path, sum, filepath.Join(root, "install")); err == nil {
		t.Fatal("existing version overwritten")
	}
	path, sum, root = testBundle(t, "../escape")
	if _, err = Install(path, sum, filepath.Join(root, "install")); err == nil {
		t.Fatal("path traversal accepted")
	}
}
func TestMCPConfigPreservesOtherServersAndRequiresExplicitReplacement(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	config := filepath.Join(root, "mcp_servers.json")
	original := []byte(`{"other_setting":true,"servers":[{"name":"other","command":"keep-me"}]}`)
	os.WriteFile(config, original, 0600)
	if err := WriteConfig(config, "/absolute/mcp", false); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Other   bool                             `json:"other_setting"`
		Servers []struct{ Name, Command string } `json:"servers"`
	}
	data, _ := os.ReadFile(config)
	json.Unmarshal(data, &doc)
	if !doc.Other || len(doc.Servers) != 2 || doc.Servers[0].Command != "keep-me" {
		t.Fatal("existing configuration changed")
	}
	backup, _ := os.ReadFile(config + ".backup")
	if !bytes.Equal(original, backup) {
		t.Fatal("backup does not match original")
	}
	if WriteConfig(config, "/another/mcp", false) == nil {
		t.Fatal("implicit replacement accepted")
	}
}
