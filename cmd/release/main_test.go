package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMinimumAriaVersionIsTheHighestPluginMinimum(t *testing.T) {
	root := t.TempDir()
	for name, minimum := range map[string]string{"a": "0.140.0", "b": "0.165.0", "c": "0.99.10"} {
		dir := filepath.Join(root, "plugins", name)
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"minimum_aria_version":"`+minimum+`"}`), 0644)
	}
	if got, err := minimumAriaVersion(root); err != nil || got != "0.165.0" {
		t.Fatal(got, err)
	}
	os.WriteFile(filepath.Join(root, "plugins", "c", "package.json"), []byte(`{"minimum_aria_version":"latest"}`), 0644)
	if _, err := minimumAriaVersion(root); err == nil {
		t.Fatal("invalid minimum accepted")
	}
}
