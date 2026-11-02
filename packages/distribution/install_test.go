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
	for _, directory := range []string{".claude-plugin", "plugins", "skills"} {
		err := filepath.WalkDir(filepath.Join("..", "..", directory), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			name, err := filepath.Rel(filepath.Join("..", ".."), path)
			if err != nil {
				return err
			}
			files[filepath.ToSlash(name)] = data
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
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
	if _, err := os.Stat(filepath.Join(i.Directory, "skills", "computer-use", "SKILL.md")); err != nil {
		t.Fatal("computer-use workflow missing from installed multi-plugin catalogue", err)
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
	backups, _ := filepath.Glob(config + ".backup-*")
	if len(backups) != 1 {
		t.Fatal("expected one backup", backups)
	}
	backup, _ := os.ReadFile(backups[0])
	if !bytes.Equal(original, backup) {
		t.Fatal("backup does not match original")
	}
	if WriteConfig(config, "/another/mcp", false) == nil {
		t.Fatal("implicit replacement accepted")
	}
}

func TestUpgradeKeepsEveryBackupAndAFailedRegistrationCanBeRetried(t *testing.T) {
	path, sum, root := testBundle(t, "")
	config := filepath.Join(root, "mcp_servers.json")
	original := []byte(`{"servers":[{"name":"other","command":"keep-me"}]}`)
	os.WriteFile(config, original, 0600)
	// A backup left by an earlier installer must neither block nor be replaced.
	legacy := []byte("legacy backup")
	os.WriteFile(config+".backup", legacy, 0600)

	first, err := InstallAndRegister(path, sum, filepath.Join(root, "first"), config, true)
	if err != nil {
		t.Fatal(err)
	}
	afterFirst, _ := os.ReadFile(config)
	// install.sh always passes --replace; the second upgrade (same second, so
	// the timestamp collides) must still register and keep both backups.
	second, err := InstallAndRegister(path, sum, filepath.Join(root, "second"), config, true)
	if err != nil {
		t.Fatal("second registration failed:", err)
	}
	if kept, _ := os.ReadFile(config + ".backup"); !bytes.Equal(kept, legacy) {
		t.Fatal("earlier backup overwritten")
	}
	backups, _ := filepath.Glob(config + ".backup-*")
	if len(backups) != 2 {
		t.Fatal("each upgrade needs its own backup", backups)
	}
	var contents [][]byte
	for _, name := range backups {
		data, _ := os.ReadFile(name)
		contents = append(contents, data)
	}
	if !(bytes.Equal(contents[0], original) && bytes.Equal(contents[1], afterFirst)) && !(bytes.Equal(contents[1], original) && bytes.Equal(contents[0], afterFirst)) {
		t.Fatal("backups do not hold the previous configurations")
	}
	var doc struct {
		Servers []struct{ Name, Command string } `json:"servers"`
	}
	data, _ := os.ReadFile(config)
	json.Unmarshal(data, &doc)
	if len(doc.Servers) != 2 || doc.Servers[1].Command != second.Binary || doc.Servers[1].Command == first.Binary {
		t.Fatal("upgrade did not point Aria at the new version", doc.Servers)
	}

	// A failed registration leaves no half-installed version behind.
	broken := filepath.Join(root, "broken.json")
	os.WriteFile(broken, []byte("{not json"), 0600)
	destination := filepath.Join(root, "retry")
	if _, err = InstallAndRegister(path, sum, destination, broken, true); err == nil {
		t.Fatal("invalid configuration accepted")
	}
	if _, err = os.Stat(filepath.Join(destination, "0.4.0")); !os.IsNotExist(err) {
		t.Fatal("failed registration left the version installed", err)
	}
	os.WriteFile(broken, original, 0600)
	if _, err = InstallAndRegister(path, sum, destination, broken, true); err != nil {
		t.Fatal("re-running after a failed registration failed:", err)
	}
}
