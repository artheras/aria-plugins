package catalog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/artheras/aria-plugins/packages/sdk"
)

// The repository's own plugins are checked against their published contract.
// There is no vendored JSON Schema library, so validate implements the subset
// of keywords contracts/plugin-package.schema.json uses and fails on any other,
// which keeps the check honest if the schema grows.

const repoRoot = "../.."

func validate(schema map[string]any, value any, at string) []string {
	var errs []string
	for key := range schema {
		switch key {
		case "$schema", "$id", "title", "description", "type", "required", "properties", "additionalProperties", "pattern", "const", "items", "uniqueItems":
		default:
			errs = append(errs, fmt.Sprintf("%s: schema keyword %q is not supported by this check", at, key))
		}
	}
	if want, ok := schema["const"]; ok && fmt.Sprint(want) != fmt.Sprint(value) {
		errs = append(errs, fmt.Sprintf("%s: must be %v", at, want))
	}
	switch schema["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return append(errs, at+": must be an object")
		}
		for _, name := range schema["required"].([]any) {
			if _, ok := object[name.(string)]; !ok {
				errs = append(errs, fmt.Sprintf("%s: missing required %q", at, name))
			}
		}
		properties, _ := schema["properties"].(map[string]any)
		for name, item := range object {
			sub, ok := properties[name].(map[string]any)
			if !ok {
				if schema["additionalProperties"] == false {
					errs = append(errs, fmt.Sprintf("%s: unknown property %q", at, name))
				}
				continue
			}
			errs = append(errs, validate(sub, item, at+"."+name)...)
		}
	case "array":
		list, ok := value.([]any)
		if !ok {
			return append(errs, at+": must be an array")
		}
		seen := map[string]bool{}
		for n, item := range list {
			if schema["uniqueItems"] == true {
				key := fmt.Sprint(item)
				if seen[key] {
					errs = append(errs, fmt.Sprintf("%s: duplicate item %v", at, item))
				}
				seen[key] = true
			}
			if sub, ok := schema["items"].(map[string]any); ok {
				errs = append(errs, validate(sub, item, fmt.Sprintf("%s[%d]", at, n))...)
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return append(errs, at+": must be a string")
		}
		if pattern, ok := schema["pattern"].(string); ok && !regexp.MustCompile(pattern).MatchString(text) {
			errs = append(errs, fmt.Sprintf("%s: %q does not match %s", at, text, pattern))
		}
	case nil:
	default:
		errs = append(errs, fmt.Sprintf("%s: schema type %v is not supported by this check", at, schema["type"]))
	}
	return errs
}

func loadJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err = decoder.Decode(target); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestSchemaSubsetValidatorRejectsContractViolations(t *testing.T) {
	var schema map[string]any
	loadJSON(t, filepath.Join(repoRoot, "contracts", "plugin-package.schema.json"), &schema)
	for name, doc := range map[string]string{
		"missing field":  `{"name":"x","version":"1.0.0","contract_version":"1","minimum_aria_version":"0.1.0","mcp_protocol_version":"2024-11-05","tools":[],"permissions":[],"required_env":[]}`,
		"bad name":       `{"name":"X","version":"1.0.0","contract_version":"1","minimum_aria_version":"0.1.0","mcp_protocol_version":"2024-11-05","tools":[],"permissions":[],"required_env":[],"human_only_actions":[]}`,
		"wrong contract": `{"name":"x","version":"1.0.0","contract_version":"2","minimum_aria_version":"0.1.0","mcp_protocol_version":"2024-11-05","tools":[],"permissions":[],"required_env":[],"human_only_actions":[]}`,
		"duplicate tool": `{"name":"x","version":"1.0.0","contract_version":"1","minimum_aria_version":"0.1.0","mcp_protocol_version":"2024-11-05","tools":["a","a"],"permissions":[],"required_env":[],"human_only_actions":[]}`,
		"bad env":        `{"name":"x","version":"1.0.0","contract_version":"1","minimum_aria_version":"0.1.0","mcp_protocol_version":"2024-11-05","tools":[],"permissions":[],"required_env":["lower"],"human_only_actions":[]}`,
		"unknown field":  `{"name":"x","version":"1.0.0","contract_version":"1","minimum_aria_version":"0.1.0","mcp_protocol_version":"2024-11-05","tools":[],"permissions":[],"required_env":[],"human_only_actions":[],"extra":1}`,
		"wrong type":     `{"name":"x","version":"1.0.0","contract_version":"1","minimum_aria_version":"0.1.0","mcp_protocol_version":"2024-11-05","tools":"a","permissions":[],"required_env":[],"human_only_actions":[]}`,
	} {
		var value any
		json.Unmarshal([]byte(doc), &value)
		if len(validate(schema, value, "package")) == 0 {
			t.Errorf("%s accepted", name)
		}
	}
}

// server is one host template's view of a plugin's MCP server.
type server struct {
	name, command string
	args          []string
	env           map[string]string
}

// parseCodexTOML reads the small TOML subset the codex.toml templates use:
// [section] headers and key = "string" or key = ["string", ...] lines.
func parseCodexTOML(t *testing.T, path string) []server {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	byName := map[string]*server{}
	var order []string
	var current *server
	var inEnv bool
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if strings.HasPrefix(text, "[") {
			parts := strings.Split(strings.Trim(text, "[]"), ".")
			if parts[0] != "mcp_servers" || len(parts) < 2 || len(parts) > 3 || (len(parts) == 3 && parts[2] != "env") {
				t.Fatalf("%s:%d: unexpected section %s", path, line, text)
			}
			if byName[parts[1]] == nil {
				byName[parts[1]] = &server{name: parts[1], env: map[string]string{}}
				order = append(order, parts[1])
			}
			current, inEnv = byName[parts[1]], len(parts) == 3
			continue
		}
		key, raw, ok := strings.Cut(text, "=")
		if !ok || current == nil {
			t.Fatalf("%s:%d: unexpected line %s", path, line, text)
		}
		key, raw = strings.TrimSpace(key), strings.TrimSpace(raw)
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatalf("%s:%d: only JSON-compatible strings and string arrays are expected: %v", path, line, err)
		}
		switch {
		case inEnv:
			current.env[key], _ = value.(string)
		case key == "command":
			current.command, _ = value.(string)
		case key == "args":
			for _, item := range value.([]any) {
				current.args = append(current.args, item.(string))
			}
		default:
			t.Fatalf("%s:%d: unexpected key %s", path, line, key)
		}
	}
	var servers []server
	for _, name := range order {
		servers = append(servers, *byName[name])
	}
	return servers
}

func templates(t *testing.T, dir string) map[string][]server {
	t.Helper()
	found := map[string][]server{}
	if _, err := os.Stat(filepath.Join(dir, "mcp.json")); err == nil {
		var doc struct {
			Servers []struct {
				Name    string            `json:"name"`
				Command string            `json:"command"`
				Args    []string          `json:"args"`
				Env     map[string]string `json:"env"`
			} `json:"servers"`
		}
		loadJSON(t, filepath.Join(dir, "mcp.json"), &doc)
		for _, s := range doc.Servers {
			found["mcp.json"] = append(found["mcp.json"], server{s.Name, s.Command, s.Args, s.Env})
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); err == nil {
		var doc map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		}
		loadJSON(t, filepath.Join(dir, ".mcp.json"), &doc)
		names := make([]string, 0, len(doc))
		for name := range doc {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			s := doc[name]
			found[".mcp.json"] = append(found[".mcp.json"], server{name, s.Command, s.Args, s.Env})
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "codex.toml")); err == nil {
		found["codex.toml"] = parseCodexTOML(t, filepath.Join(dir, "codex.toml"))
	}
	return found
}

func TestPluginPackagesMatchContractAndHostTemplates(t *testing.T) {
	var schema map[string]any
	loadJSON(t, filepath.Join(repoRoot, "contracts", "plugin-package.schema.json"), &schema)
	var marketplace struct {
		Metadata struct{ Version string }         `json:"metadata"`
		Plugins  []struct{ Name, Version string } `json:"plugins"`
	}
	loadJSON(t, filepath.Join(repoRoot, ".claude-plugin", "marketplace.json"), &marketplace)
	if marketplace.Metadata.Version != sdk.Version {
		t.Errorf("marketplace metadata version %s differs from release version %s", marketplace.Metadata.Version, sdk.Version)
	}
	listed := map[string]string{}
	for _, p := range marketplace.Plugins {
		listed[p.Name] = p.Version
	}
	packages, err := filepath.Glob(filepath.Join(repoRoot, "plugins", "*", "package.json"))
	if err != nil || len(packages) == 0 {
		t.Fatal("no plugin packages found", err)
	}
	reference := regexp.MustCompile(`^\$\{([A-Z][A-Z0-9_]+)\}$`)
	for _, path := range packages {
		dir := filepath.Dir(path)
		plugin := filepath.Base(dir)
		t.Run(plugin, func(t *testing.T) {
			var raw any
			loadJSON(t, path, &raw)
			for _, problem := range validate(schema, raw, "package.json") {
				t.Error(problem)
			}
			var pkg struct {
				Name        string   `json:"name"`
				Version     string   `json:"version"`
				Tools       []string `json:"tools"`
				RequiredEnv []string `json:"required_env"`
				OptionalEnv []string `json:"optional_env"`
			}
			loadJSON(t, path, &pkg)
			if pkg.Name != plugin {
				t.Errorf("package name %q does not match folder %q", pkg.Name, plugin)
			}
			var manifest struct{ Version string }
			loadJSON(t, filepath.Join(dir, ".aria-plugin", "plugin.json"), &manifest)
			if manifest.Version != pkg.Version || listed[plugin] != pkg.Version {
				t.Errorf("version drift: package %s, plugin.json %s, marketplace %s", pkg.Version, manifest.Version, listed[plugin])
			}

			// Host templates: one consistent server, declared environment only,
			// and values that defer to the host instead of pinning a literal.
			declared := append(slices.Clone(pkg.RequiredEnv), pkg.OptionalEnv...)
			found := templates(t, dir)
			if len(found) == 0 {
				t.Fatal("no MCP host templates")
			}
			var first *server
			for file, servers := range found {
				if len(servers) != 1 {
					t.Fatalf("%s: expected exactly one server, got %d", file, len(servers))
				}
				s := servers[0]
				if first == nil {
					first = &s
				} else if s.name != first.name || filepath.Base(s.command) != filepath.Base(first.command) || !slices.Equal(s.args, first.args) {
					t.Errorf("%s disagrees with the other templates: %+v vs %+v", file, s, *first)
				}
				for _, key := range pkg.RequiredEnv {
					if _, ok := s.env[key]; !ok {
						t.Errorf("%s: required %s is not passed to the server", file, key)
					}
				}
				for key, value := range s.env {
					if !slices.Contains(declared, key) {
						t.Errorf("%s: env %s is not declared in package.json", file, key)
					}
					if m := reference.FindStringSubmatch(value); m == nil || m[1] != key {
						t.Errorf("%s: env %s=%q overrides the host environment; use ${%s} or omit it", file, key, value, key)
					}
				}
			}

			// Tools: the skill documents each one, and its policy narrows to them.
			for _, skill := range mustGlob(t, filepath.Join(dir, "skills", "*")) {
				text, err := os.ReadFile(filepath.Join(skill, "SKILL.md"))
				if err != nil {
					t.Fatal(err)
				}
				for _, tool := range pkg.Tools {
					if !strings.Contains(string(text), tool) {
						t.Errorf("%s does not document tool %s", filepath.Base(skill), tool)
					}
				}
				var policy struct {
					SchemaVersion string   `json:"schema_version"`
					AllowedTools  []string `json:"allowed_tools"`
				}
				loadJSON(t, filepath.Join(skill, "skill-policy.json"), &policy)
				if policy.SchemaVersion != "aria.skill-policy.v1" {
					t.Errorf("%s: skill-policy.json schema_version is %q", filepath.Base(skill), policy.SchemaVersion)
				}
				for _, tool := range policy.AllowedTools {
					if !slices.Contains(pkg.Tools, tool) {
						t.Errorf("%s: allowed tool %s is not a package tool", filepath.Base(skill), tool)
					}
				}
			}
		})
	}
}

func TestFinanceSkillNarrowsToEveryFinanceTool(t *testing.T) {
	var pkg struct {
		Tools []string `json:"tools"`
	}
	loadJSON(t, filepath.Join(repoRoot, "plugins", "finance", "package.json"), &pkg)
	var policy struct {
		AllowedTools []string `json:"allowed_tools"`
	}
	loadJSON(t, filepath.Join(repoRoot, "plugins", "finance", "skills", "finance", "skill-policy.json"), &policy)
	if !slices.Equal(pkg.Tools, policy.AllowedTools) {
		t.Fatalf("finance allowed_tools %v differ from package tools %v", policy.AllowedTools, pkg.Tools)
	}
}

func TestCommerceBridgeImplementsExactlyThePackageTools(t *testing.T) {
	var pkg struct {
		Tools []string `json:"tools"`
	}
	loadJSON(t, filepath.Join(repoRoot, "plugins", "commerce", "package.json"), &pkg)
	source, err := os.ReadFile(filepath.Join(repoRoot, "cmd", "commerce-mcp", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	var tools []string
	for _, m := range regexp.MustCompile(`"name": "([a-z_]+)"`).FindAllStringSubmatch(string(source), -1) {
		tools = append(tools, m[1])
	}
	if len(tools) == 0 || !slices.Equal(tools, pkg.Tools) {
		t.Fatalf("MCP bridge tools %v differ from package tools %v", tools, pkg.Tools)
	}
}

func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		t.Fatal("nothing matches", pattern, err)
	}
	return matches
}
