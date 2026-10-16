// Package catalog validates the Aria-compatible plugin catalogue and builds its
// portable skills mirror. It never installs tools or modifies a user's Aria home.
package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Entry struct {
	Name        string   `json:"name"`
	Source      string   `json:"source"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Skills      []string `json:"skills"`
}

type Marketplace struct {
	Name  string `json:"name"`
	Owner struct {
		Name string `json:"name"`
	} `json:"owner"`
	Plugins []Entry `json:"plugins"`
}

type manifest struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Entrypoints map[string]string `json:"entrypoints"`
}

type lockEntry struct {
	SHA256 string `json:"sha256"`
}

type lockFile struct {
	Version int                  `json:"version"`
	Skills  map[string]lockEntry `json:"skills"`
}

type skill struct {
	directory string
	qualified string
	files     map[string][]byte
	hash      string
}

var slug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
var version = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// Refuse symlinks, including parent components, before reading or replacing
// generated content. Catalogue entries are repository-local paths only.
func localPath(root, relative string) (string, error) {
	if filepath.IsAbs(relative) || strings.Contains(relative, "\\") {
		return "", fmt.Errorf("non-local path: %s", relative)
	}
	parts := strings.Split(strings.TrimPrefix(relative, "./"), "/")
	current := root
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid path component in %s", relative)
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlinks are not catalogue content: %s", current)
		}
	}
	return current, nil
}

func ignored(name string) bool {
	return name == ".DS_Store" || name == ".pytest_cache" || name == "__pycache__"
}

func treeFiles(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ignored(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in skill tree: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular skill file: %s", path)
		}
		if filepath.Ext(path) == ".pyc" {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = data
		return nil
	})
	return files, err
}

// Python's sorted(Path) compares path components. Sorting the entire path
// string would change the order of a/x and a.md and break Aria's lock hash.
func treeHash(files map[string][]byte) string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		a, b := strings.Split(paths[i], "/"), strings.Split(paths[j], "/")
		for n := 0; n < len(a) && n < len(b); n++ {
			if a[n] != b[n] {
				return a[n] < b[n]
			}
		}
		return len(a) < len(b)
	})
	digest := sha256.New()
	for _, path := range paths {
		digest.Write([]byte(path))
		digest.Write([]byte{0})
		digest.Write(files[path])
		digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func skillName(data []byte) (string, error) {
	// Parse with LF endings; the hash still covers the file's real bytes.
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if len(data) == 0 || len(data) > 20000 || !bytes.HasPrefix(data, []byte("---\n")) {
		return "", fmt.Errorf("SKILL.md needs frontmatter and must be at most 20000 bytes")
	}
	end := bytes.Index(data[4:], []byte("\n---\n"))
	if end < 0 || len(bytes.TrimSpace(data[4+end+5:])) == 0 {
		return "", fmt.Errorf("SKILL.md needs closed frontmatter and instructions")
	}
	name, description := "", ""
	for _, line := range strings.Split(string(data[4:4+end]), "\n") {
		if strings.HasPrefix(line, "name:") {
			name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), "\"'")
		}
		if strings.HasPrefix(line, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}
	if !slug.MatchString(name) || description == "" {
		return "", fmt.Errorf("SKILL.md needs a valid name and description")
	}
	return name, nil
}

func plan(root string) ([]skill, error) {
	marketPath, err := localPath(root, ".claude-plugin/marketplace.json")
	if err != nil {
		return nil, err
	}
	var market Marketplace
	if err := readJSON(marketPath, &market); err != nil {
		return nil, err
	}
	if market.Name != "aria-plugins" || market.Owner.Name == "" || len(market.Plugins) == 0 {
		return nil, fmt.Errorf("marketplace needs aria-plugins name, owner and plugins")
	}
	seenNames, seenDirectories := map[string]bool{}, map[string]bool{}
	var skills []skill
	for _, entry := range market.Plugins {
		if !slug.MatchString(entry.Name) || seenNames[entry.Name] || !version.MatchString(entry.Version) || entry.Description == "" {
			return nil, fmt.Errorf("invalid or duplicate plugin: %s", entry.Name)
		}
		seenNames[entry.Name] = true
		if entry.Source != "./plugins/"+entry.Name || len(entry.Skills) == 0 {
			return nil, fmt.Errorf("%s: expected local plugin source and skills", entry.Name)
		}
		pluginRoot, err := localPath(root, entry.Source)
		if err != nil {
			return nil, err
		}
		for _, relative := range []string{".aria-plugin/plugin.json", ".claude-plugin/plugin.json"} {
			path, err := localPath(pluginRoot, relative)
			if err != nil {
				return nil, err
			}
			var manifest manifest
			if err := readJSON(path, &manifest); err != nil {
				return nil, err
			}
			if manifest.Name != entry.Name || manifest.Version != entry.Version || manifest.Description == "" {
				return nil, fmt.Errorf("%s: name/version differs from catalogue", path)
			}
			for _, relative := range manifest.Entrypoints {
				path, err := localPath(pluginRoot, strings.TrimSuffix(relative, "/"))
				if err != nil {
					return nil, err
				}
				if _, err := os.Stat(path); err != nil {
					return nil, err
				}
			}
		}
		readme, err := localPath(pluginRoot, "README.md")
		if err != nil {
			return nil, err
		}
		if data, err := os.ReadFile(readme); err != nil || len(bytes.TrimSpace(data)) == 0 {
			return nil, fmt.Errorf("%s: missing plugin README", entry.Name)
		}
		for _, relative := range entry.Skills {
			directory := strings.TrimPrefix(relative, "./skills/")
			if relative != "./skills/"+directory || !slug.MatchString(directory) || seenDirectories[directory] {
				return nil, fmt.Errorf("invalid or duplicated skill path: %s", relative)
			}
			seenDirectories[directory] = true
			path, err := localPath(pluginRoot, relative)
			if err != nil {
				return nil, err
			}
			files, err := treeFiles(path)
			if err != nil {
				return nil, err
			}
			name, err := skillName(files["SKILL.md"])
			if err != nil || name != directory {
				return nil, fmt.Errorf("%s: skill name must match directory and have valid frontmatter", path)
			}
			skills = append(skills, skill{directory, entry.Name + ":" + name, files, treeHash(files)})
		}
	}
	return skills, nil
}

// Sync copies canonical plugin skills to the root discovered by Aria's current
// installer, then records hashes using exactly Aria's path/NUL/content algorithm.
func Sync(root string) error {
	skills, err := plan(root)
	if err != nil {
		return err
	}
	locks := lockFile{Version: 1, Skills: map[string]lockEntry{}}
	for _, skill := range skills {
		destination, err := localPath(root, "skills/"+skill.directory)
		if err != nil {
			return err
		}
		// Validate the existing generated tree before replacing it.
		if _, err := os.Lstat(destination); err == nil {
			if _, err := treeFiles(destination); err != nil {
				return err
			}
		}
		if err := os.RemoveAll(destination); err != nil {
			return err
		}
		for path, data := range skill.files {
			target := filepath.Join(destination, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(target, data, 0644); err != nil {
				return err
			}
		}
		locks.Skills[skill.qualified] = lockEntry{skill.hash}
	}
	lockPath, err := localPath(root, ".claude-plugin/skills.lock.json")
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(locks, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(lockPath, append(data, '\n'), 0644)
}

// Check rejects changed, missing, stale or unregistered generated skills.
func Check(root string) error {
	skills, err := plan(root)
	if err != nil {
		return err
	}
	lockPath, err := localPath(root, ".claude-plugin/skills.lock.json")
	if err != nil {
		return err
	}
	var locks lockFile
	if err := readJSON(lockPath, &locks); err != nil {
		return err
	}
	if locks.Version != 1 || len(locks.Skills) != len(skills) {
		return fmt.Errorf("lock has missing or extra skills; run make catalog-sync")
	}
	expected := map[string]bool{}
	for _, skill := range skills {
		expected[skill.directory] = true
		if locks.Skills[skill.qualified].SHA256 != skill.hash {
			return fmt.Errorf("%s: canonical content differs from lock; run make catalog-sync", skill.qualified)
		}
		path, err := localPath(root, "skills/"+skill.directory)
		if err != nil {
			return err
		}
		files, err := treeFiles(path)
		if err != nil {
			return err
		}
		if treeHash(files) != skill.hash {
			return fmt.Errorf("%s: portable mirror differs from canonical plugin", skill.qualified)
		}
	}
	rootSkills, err := localPath(root, "skills")
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(rootSkills)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !ignored(entry.Name()) && (entry.Type()&os.ModeSymlink != 0 || entry.IsDir() && !expected[entry.Name()]) {
			return fmt.Errorf("unregistered root skill: %s", entry.Name())
		}
	}
	return nil
}
