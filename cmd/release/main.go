// Build reproducible versioned release bundles; publishing is handled by CI.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/artheras/aria-plugins/packages/catalog"
	"github.com/artheras/aria-plugins/packages/distribution"
	"github.com/artheras/aria-plugins/packages/sdk"
)

// minimumAriaVersion is the highest minimum_aria_version declared by the
// bundled plugins, so the bundle never claims to work on a host one of its
// plugins does not support.
func minimumAriaVersion(root string) (string, error) {
	paths, err := filepath.Glob(filepath.Join(root, "plugins", "*", "package.json"))
	if err != nil || len(paths) == 0 {
		return "", fmt.Errorf("no plugin package.json found")
	}
	best, bestParts := "", [3]int{-1, -1, -1}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		var pkg struct {
			Minimum string `json:"minimum_aria_version"`
		}
		if err = json.Unmarshal(data, &pkg); err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		if !distribution.VersionPattern.MatchString(pkg.Minimum) {
			return "", fmt.Errorf("%s: invalid minimum_aria_version %q", path, pkg.Minimum)
		}
		var parts [3]int
		fmt.Sscanf(pkg.Minimum, "%d.%d.%d", &parts[0], &parts[1], &parts[2])
		if slices.Compare(parts[:], bestParts[:]) > 0 {
			best, bestParts = pkg.Minimum, parts
		}
	}
	return best, nil
}

func build(root, out, platform string) (string, error) {
	parts := strings.Split(platform, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid platform %q", platform)
	}
	goos, arch := parts[0], parts[1]
	if (goos != "darwin" && goos != "linux" && goos != "windows") || (arch != "amd64" && arch != "arm64") {
		return "", fmt.Errorf("unsupported platform")
	}
	tmp, err := os.MkdirTemp("", "aria-release-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	files := map[string]string{}
	for binary, source := range map[string]string{"aria-commerce-mcp": "./cmd/commerce-mcp", "aria-plugins": "./cmd/aria-plugins"} {
		name := binary + distribution.Extension(goos)
		path := filepath.Join(tmp, name)
		command := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", path, source)
		command.Dir = root
		for _, v := range os.Environ() {
			if !strings.HasPrefix(v, "GOOS=") && !strings.HasPrefix(v, "GOARCH=") && !strings.HasPrefix(v, "CGO_ENABLED=") {
				command.Env = append(command.Env, v)
			}
		}
		command.Env = append(command.Env, "GOOS="+goos, "GOARCH="+arch, "CGO_ENABLED=0")
		if output, err := command.CombinedOutput(); err != nil {
			return "", fmt.Errorf("%s build failed: %s", name, output)
		}
		files["bin/"+name] = path
	}
	for _, dir := range []string{"plugins/commerce", "skills/commerce", "plugins/finance", "skills/finance", "plugins/computer-use", "skills/computer-use", ".claude-plugin", "contracts"} {
		if err = filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("release source contains symlink")
			}
			if d.IsDir() {
				return nil
			}
			relative, _ := filepath.Rel(root, path)
			files[filepath.ToSlash(relative)] = path
			return nil
		}); err != nil {
			return "", err
		}
	}
	files["README.md"] = filepath.Join(root, "docs", "bundle-readme.md")
	files["LICENSE"] = filepath.Join(root, "LICENSE")
	minimum, err := minimumAriaVersion(root)
	if err != nil {
		return "", err
	}
	manifest := distribution.Manifest{Name: "commerce", Version: sdk.Version, ContractVersion: sdk.ContractVersion, GOOS: goos, GOARCH: arch, MinimumAriaVersion: minimum, RequiredEnv: []string{"ARIA_COMMERCE_URL", "ARIA_MERCHANT_TOKEN"}}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	manifestPath := filepath.Join(tmp, "bundle.json")
	os.WriteFile(manifestPath, append(data, '\n'), 0644)
	files["bundle.json"] = manifestPath
	name := distribution.Name(sdk.Version, goos, arch)
	target := filepath.Join(out, name)
	file, err := os.Create(target)
	if err != nil {
		return "", err
	}
	writer := zip.NewWriter(file)
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		info, err := os.Stat(files[path])
		if err != nil {
			file.Close()
			return "", err
		}
		header := &zip.FileHeader{Name: path, Method: zip.Deflate}
		header.SetMode(0644)
		if strings.HasPrefix(path, "bin/") {
			header.SetMode(0755)
		}
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		out, err := writer.CreateHeader(header)
		if err != nil {
			file.Close()
			return "", err
		}
		input, err := os.Open(files[path])
		if err != nil {
			file.Close()
			return "", err
		}
		_, err = io.Copy(out, input)
		input.Close()
		if err != nil {
			file.Close()
			return "", err
		}
		_ = info
	}
	if err = writer.Close(); err != nil {
		file.Close()
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	input, _ := os.Open(target)
	defer input.Close()
	h := sha256.New()
	if _, err = io.Copy(h, input); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)) + "  " + name, nil
}
func run() error {
	out := flag.String("out", "dist", "output directory")
	platforms := flag.String("platforms", "darwin/arm64,darwin/amd64,linux/amd64,linux/arm64,windows/amd64,windows/arm64", "comma-separated target platforms")
	version := flag.String("version", sdk.Version, "release version, must match source")
	flag.Parse()
	if *version != sdk.Version {
		return fmt.Errorf("release tag and source version disagree")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if err = catalog.Check(root); err != nil {
		return err
	}
	if err = os.MkdirAll(*out, 0755); err != nil {
		return err
	}
	var sums []string
	for _, platform := range strings.Split(*platforms, ",") {
		sum, err := build(root, *out, platform)
		if err != nil {
			return err
		}
		sums = append(sums, sum)
		fmt.Println(sum)
	}
	sort.Strings(sums)
	return os.WriteFile(filepath.Join(*out, "SHA256SUMS"), []byte(strings.Join(sums, "\n")+"\n"), 0644)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
