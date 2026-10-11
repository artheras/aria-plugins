package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root, path, content string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, root, ".claude-plugin/marketplace.json", `{
		"name":"aria-plugins", "owner":{"name":"Arthera"}, "plugins":[{
		"name":"sample", "version":"0.1.0", "description":"A sample plugin",
		"source":"./plugins/sample", "skills":["./skills/sample"]}]}`)
	for _, path := range []string{".aria-plugin/plugin.json", ".claude-plugin/plugin.json"} {
		put(t, root, "plugins/sample/"+path, `{"name":"sample","version":"0.1.0","description":"A sample plugin"}`)
	}
	put(t, root, "plugins/sample/README.md", "# Sample plugin\n")
	put(t, root, "plugins/sample/skills/sample/SKILL.md", "---\nname: sample\ndescription: Sample workflow\n---\n\nInspect a sample.\n")
	put(t, root, "plugins/sample/skills/sample/references/a.txt", "reference\n")
	if err := Sync(root); err != nil {
		t.Fatal(err)
	}
	if err := Check(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCheckDetectsTampering(t *testing.T) {
	for _, path := range []string{
		"plugins/sample/skills/sample/references/a.txt",
		"skills/sample/references/a.txt",
		".claude-plugin/skills.lock.json",
	} {
		t.Run(path, func(t *testing.T) {
			root := fixture(t)
			put(t, root, path, "changed\n")
			if err := Check(root); err == nil {
				t.Fatal("changed catalogue accepted")
			}
			if err := Sync(root); err != nil {
				t.Fatal(err)
			}
			if err := Check(root); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckRejectsUnregisteredAndMissingSkills(t *testing.T) {
	root := fixture(t)
	put(t, root, "skills/unregistered/SKILL.md", "unregistered")
	if err := Check(root); err == nil {
		t.Fatal("unregistered skill accepted")
	}
	if err := os.RemoveAll(filepath.Join(root, "skills/unregistered")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "skills/sample/SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := Check(root); err == nil {
		t.Fatal("missing mirror accepted")
	}
}

func TestPlanRejectsEscapingPathsAndVersionMismatch(t *testing.T) {
	for _, value := range []string{"./skills/../outside", "./skills/sample/../../outside"} {
		root := fixture(t)
		path := filepath.Join(root, ".claude-plugin/marketplace.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		put(t, root, ".claude-plugin/marketplace.json", strings.ReplaceAll(string(data), "./skills/sample", value))
		if err := Sync(root); err == nil {
			t.Fatalf("escaping skill path accepted: %s", value)
		}
	}
	root := fixture(t)
	put(t, root, "plugins/sample/.aria-plugin/plugin.json", `{"name":"sample","version":"0.2.0","description":"Sample"}`)
	if err := Check(root); err == nil {
		t.Fatal("manifest version mismatch accepted")
	}
}

func TestSyncRefusesSymlinkWithoutChangingTarget(t *testing.T) {
	root := fixture(t)
	outside := t.TempDir()
	put(t, outside, "keep.txt", "keep")
	mirror := filepath.Join(root, "skills/sample")
	if err := os.RemoveAll(mirror); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, mirror); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := Sync(root); err == nil {
		t.Fatal("symlink accepted")
	}
	data, err := os.ReadFile(filepath.Join(outside, "keep.txt"))
	if err != nil || string(data) != "keep" {
		t.Fatal("outside target modified")
	}
}

func TestHashMatchesAriaPythonPathOrdering(t *testing.T) {
	files := map[string][]byte{"a/x.txt": []byte("nested"), "a.md": []byte("flat"), "z.txt": []byte("last")}
	// Produced by Aria's SHA-256(path + NUL + bytes + NUL), sorted(Path).
	const expected = "d8cbed85a2016bf77dc37bd9e042cc099c2d305c0127c10cde60eb8f0b8b6a1c"
	if got := treeHash(files); got != expected {
		t.Fatalf("Go hash %s differs from Aria Python hash %s", got, expected)
	}
	root := t.TempDir()
	put(t, root, "a.md", "flat")
	put(t, root, "a/x.txt", "nested")
	put(t, root, "z.txt", "last")
	put(t, root, ".DS_Store", "ignored")
	put(t, root, "__pycache__/test.pyc", "ignored")
	loaded, err := treeFiles(root)
	if err != nil || treeHash(loaded) != expected {
		t.Fatal("ignored files changed the hash", err)
	}
}
