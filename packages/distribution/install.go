// Package distribution validates versioned plugin bundles and merges MCP
// configuration without executing a downloaded binary or storing credentials.
package distribution

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/artheras/aria-plugins/packages/catalog"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

type Manifest struct {
	Name               string   `json:"name"`
	Version            string   `json:"version"`
	ContractVersion    string   `json:"contract_version"`
	GOOS               string   `json:"os"`
	GOARCH             string   `json:"arch"`
	MinimumAriaVersion string   `json:"minimum_aria_version"`
	RequiredEnv        []string `json:"required_env"`
}
type Installation struct {
	Manifest      Manifest `json:"manifest"`
	Directory     string   `json:"directory"`
	Binary        string   `json:"binary"`
	ArchiveSHA256 string   `json:"archive_sha256"`
}

var VersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func Name(version, goos, goarch string) string {
	return fmt.Sprintf("aria-plugins_%s_%s_%s.zip", version, goos, goarch)
}
func Extension(goos string) string {
	if goos == "windows" {
		return ".exe"
	}
	return ""
}

func safeParents(target string) error {
	for p := target; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
			return errors.New("installation/configuration parent must be a real directory")
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if filepath.Dir(p) == p {
			return nil
		}
	}
}
func Install(archive, digest, destination string) (Installation, error) {
	result := Installation{}
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(digest) {
		return result, errors.New("provide the published lowercase SHA-256 checksum")
	}
	file, err := os.Open(archive)
	if err != nil {
		return result, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() > 200<<20 {
		return result, errors.New("bundle exceeds 200 MiB")
	}
	h := sha256.New()
	if _, err = io.Copy(h, file); err != nil {
		return result, err
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return result, errors.New("bundle checksum mismatch")
	}
	z, err := zip.NewReader(file, info.Size())
	if err != nil {
		return result, err
	}
	if len(z.File) > 200 {
		return result, errors.New("too many bundle entries")
	}
	entries := map[string]*zip.File{}
	var total uint64
	for _, entry := range z.File {
		name := entry.Name
		if name == "" || path.Clean(name) != name || strings.Contains(name, "\\") || strings.Contains(name, ":") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") || !entry.Mode().IsRegular() || entries[name] != nil {
			return result, errors.New("bundle contains unsafe or duplicated paths")
		}
		if !(strings.HasPrefix(name, "bin/") || strings.HasPrefix(name, "contracts/") || strings.HasPrefix(name, "plugins/commerce/") || strings.HasPrefix(name, "skills/commerce/") || strings.HasPrefix(name, ".claude-plugin/") || name == "bundle.json" || name == "README.md" || name == "LICENSE") {
			return result, errors.New("bundle contains unregistered components")
		}
		total += entry.UncompressedSize64
		if total > 250<<20 || entry.UncompressedSize64 > 100<<20 {
			return result, errors.New("expanded bundle exceeds limits")
		}
		entries[name] = entry
	}
	entry := entries["bundle.json"]
	if entry == nil || entry.UncompressedSize64 > 16<<10 {
		return result, errors.New("bundle manifest missing or oversized")
	}
	reader, err := entry.Open()
	if err != nil {
		return result, err
	}
	err = json.NewDecoder(io.LimitReader(reader, 16<<10)).Decode(&result.Manifest)
	reader.Close()
	m := result.Manifest
	if err != nil || m.Name != "commerce" || !VersionPattern.MatchString(m.Version) || m.ContractVersion != "1" || m.GOOS != runtime.GOOS || m.GOARCH != runtime.GOARCH {
		return result, errors.New("bundle version, contract or platform is incompatible")
	}
	binaryName := "bin/aria-commerce-mcp" + Extension(m.GOOS)
	if entries[binaryName] == nil || entries["plugins/commerce/skills/commerce/SKILL.md"] == nil || entries[".claude-plugin/skills.lock.json"] == nil {
		return result, errors.New("bundle components missing")
	}
	root, err := filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	if err = safeParents(root); err != nil {
		return result, err
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return result, err
	}
	target := filepath.Join(root, m.Version)
	if _, err = os.Lstat(target); !os.IsNotExist(err) {
		return result, errors.New("version already exists; use a fresh destination or uninstall it explicitly")
	}
	stage, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(stage)
	for name, entry := range entries {
		out := filepath.Join(stage, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			return result, err
		}
		r, err := entry.Open()
		if err != nil {
			return result, err
		}
		mode := os.FileMode(0644)
		if strings.HasPrefix(name, "bin/") {
			mode = 0755
		}
		w, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			r.Close()
			return result, err
		}
		_, err = io.Copy(w, io.LimitReader(r, 100<<20+1))
		r.Close()
		closeErr := w.Close()
		if err != nil {
			return result, err
		}
		if closeErr != nil {
			return result, closeErr
		}
	}
	if err = catalog.Check(stage); err != nil {
		return result, fmt.Errorf("bundle skill integrity failed: %w", err)
	}
	var manifestVersion struct {
		Version string `json:"version"`
	}
	manifestBytes, err := os.ReadFile(filepath.Join(stage, "plugins", "commerce", ".aria-plugin", "plugin.json"))
	if err != nil || json.Unmarshal(manifestBytes, &manifestVersion) != nil || manifestVersion.Version != m.Version {
		return result, errors.New("bundle and plugin versions disagree")
	}
	result.Directory = target
	result.Binary = filepath.Join(target, filepath.FromSlash(binaryName))
	result.ArchiveSHA256 = digest
	marker, _ := json.MarshalIndent(result, "", "  ")
	if err = os.WriteFile(filepath.Join(stage, "installation.json"), marker, 0600); err != nil {
		return result, err
	}
	if err = os.Rename(stage, target); err != nil {
		return result, err
	}
	return result, nil
}

func ReadInstallation(directory string) (Installation, error) {
	var i Installation
	root, err := filepath.Abs(directory)
	if err != nil {
		return i, err
	}
	if err = safeParents(root); err != nil {
		return i, err
	}
	data, err := os.ReadFile(filepath.Join(root, "installation.json"))
	if err != nil {
		return i, err
	}
	if json.Unmarshal(data, &i) != nil || i.Directory != root || i.Manifest.Name != "commerce" || !VersionPattern.MatchString(i.Manifest.Version) || filepath.Base(root) != i.Manifest.Version || i.Binary != filepath.Join(root, "bin", "aria-commerce-mcp"+Extension(i.Manifest.GOOS)) {
		return i, errors.New("invalid installation marker")
	}
	return i, nil
}
func WriteConfig(configPath, binary string, replace bool) error {
	full, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	if err = safeParents(filepath.Dir(full)); err != nil {
		return err
	}
	if info, err := os.Lstat(full); err == nil && !info.Mode().IsRegular() {
		return errors.New("configuration must be a regular file")
	}
	doc := map[string]json.RawMessage{}
	raw, err := os.ReadFile(full)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(raw) > 1<<20 || (len(raw) > 0 && json.Unmarshal(raw, &doc) != nil) || doc == nil {
		return errors.New("invalid MCP configuration")
	}
	var servers []map[string]any
	if existing := doc["servers"]; len(existing) > 0 && json.Unmarshal(existing, &servers) != nil {
		return errors.New("MCP servers must be an array")
	}
	entry := map[string]any{"name": "aria-commerce", "command": binary, "args": []string{}, "env": map[string]string{"ARIA_COMMERCE_URL": "${ARIA_COMMERCE_URL}", "ARIA_MERCHANT_TOKEN": "${ARIA_MERCHANT_TOKEN}"}, "enabled": true, "description": "Commerce catalogue, drafts and status; browser-only payment authority."}
	found := -1
	for n, s := range servers {
		if s["name"] == "aria-commerce" {
			if found >= 0 {
				return errors.New("duplicate Commerce MCP entries")
			}
			found = n
		}
	}
	if found >= 0 && !replace {
		return errors.New("Commerce is already configured; pass --replace after reviewing the existing entry")
	}
	if found >= 0 {
		servers[found] = entry
	} else {
		servers = append(servers, entry)
	}
	data, _ := json.Marshal(servers)
	doc["servers"] = data
	data, _ = json.MarshalIndent(doc, "", "  ")
	if err = os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		return err
	}
	if len(raw) > 0 {
		backup, err := os.OpenFile(full+".backup", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return errors.New("backup already exists or cannot be created; preserve it before retrying")
		}
		_, err = backup.Write(raw)
		backup.Close()
		if err != nil {
			return err
		}
	}
	temp, err := os.CreateTemp(filepath.Dir(full), ".mcp-")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err = temp.Chmod(0600); err == nil {
		_, err = temp.Write(append(data, '\n'))
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp.Name(), full)
}
