// aria-plugins installs verified local release bundles and registers MCP safely.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/artheras/aria-plugins/packages/distribution"
	"github.com/artheras/aria-plugins/packages/sdk"
)

func emit(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1<<20 {
		return 0, errors.New("MCP output exceeds limit")
	}
	return b.Buffer.Write(p)
}
func doctor(directory string) error {
	i, err := distribution.ReadInstallation(directory)
	if err != nil {
		return err
	}
	for _, key := range i.Manifest.RequiredEnv {
		if os.Getenv(key) == "" {
			return fmt.Errorf("missing environment variable %s", key)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, i.Binary)
	cmd.Stdin = strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{},\"clientInfo\":{\"name\":\"aria-plugins-doctor\",\"version\":\"" + sdk.Version + "\"}}}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"commerce_catalog\",\"arguments\":{}}}\n")
	var stdout limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if err = cmd.Run(); err != nil {
		return errors.New("MCP bridge failed to start; check its environment and platform")
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	seen := map[int]bool{}
	for decoder.More() {
		var response struct {
			ID     int             `json:"id"`
			Error  json.RawMessage `json:"error"`
			Result struct {
				IsError bool `json:"isError"`
				Tools   []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if decoder.Decode(&response) != nil || len(response.Error) > 0 || response.Result.IsError {
			return errors.New("MCP/service check failed; check the service URL, credential and Cloud Run identity")
		}
		if response.ID == 2 {
			tools := map[string]bool{}
			for _, tool := range response.Result.Tools {
				tools[tool.Name] = true
			}
			if len(tools) != 3 || !tools["commerce_catalog"] || !tools["commerce_draft_order"] || !tools["commerce_get_order"] {
				return errors.New("unexpected MCP tool contract")
			}
		}
		seen[response.ID] = true
	}
	if !seen[1] || !seen[2] || !seen[3] {
		return errors.New("incomplete MCP check")
	}
	emit(map[string]any{"status": "passed", "version": i.Manifest.Version, "checks": []string{"installation marker", "required environment present", "MCP initialization", "three-tool discovery", "read-only catalogue call"}, "payment_actions_exposed": false})
	return nil
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: aria-plugins install|configure|doctor|uninstall|version")
	}
	if args[0] == "version" {
		emit(map[string]string{"version": sdk.Version, "contract_version": sdk.ContractVersion})
		return nil
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	archive := flags.String("archive", "", "downloaded versioned ZIP bundle")
	checksum := flags.String("sha256", "", "published lowercase SHA-256 digest")
	directory := flags.String("dir", "", "installation destination, or installed version directory")
	config := flags.String("config", "", "explicit Aria mcp_servers.json path")
	replace := flags.Bool("replace", false, "replace the existing Commerce entry after backing up configuration")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *directory == "" {
		return errors.New("provide --dir and no positional arguments")
	}
	switch args[0] {
	case "install":
		i, err := distribution.Install(*archive, *checksum, *directory)
		if err != nil {
			return err
		}
		// Always emit a usable standalone template. Existing Aria state is touched
		// only when the user supplies its exact --config path.
		if err = distribution.WriteConfig(filepath.Join(i.Directory, "mcp_servers.json"), i.Binary, false); err != nil {
			return err
		}
		if *config != "" {
			if err = distribution.WriteConfig(*config, i.Binary, *replace); err != nil {
				emit(i)
				return fmt.Errorf("bundle installed; MCP registration needs attention: %w", err)
			}
		}
		emit(i)
		return nil
	case "configure":
		if *config == "" {
			return errors.New("provide the explicit --config path")
		}
		i, err := distribution.ReadInstallation(*directory)
		if err != nil {
			return err
		}
		if err = distribution.WriteConfig(*config, i.Binary, *replace); err != nil {
			return err
		}
		emit(map[string]any{"configured": true, "config": *config, "restart_aria_required": true})
		return nil
	case "doctor":
		return doctor(*directory)
	case "uninstall":
		i, err := distribution.ReadInstallation(*directory)
		if err != nil {
			return err
		}
		if err = os.RemoveAll(i.Directory); err != nil {
			return err
		}
		emit(map[string]any{"removed": i.Directory, "mcp_config_preserved": true, "note": "Remove or disable the Commerce entry in your MCP configuration; skills and order data are preserved."})
		return nil
	default:
		return errors.New("unknown command")
	}
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
