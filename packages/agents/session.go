package agents

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// DefaultAriaModel is the model Aria Code uses for a signed-in user.
const DefaultAriaModel = "google/gemini-3.5-flash"

// AriaGatewayURL is where Aria's OpenAI-compatible model gateway lives.
// ARIA_API_URL overrides it for staging and self-hosting, as it does in Aria Code.
func AriaGatewayURL() string {
	api := strings.TrimSpace(os.Getenv("ARIA_API_URL"))
	if api == "" {
		api = "https://api.arthera.finance"
	}
	return strings.TrimRight(api, "/") + "/api/v2/llm/v1"
}

// ariaHome mirrors aria_code.packages.aria_core.paths.aria_home: ARIA_HOME,
// else the legacy ~/.arthera when it already exists, else ~/.aria-code.
func ariaHome() (string, error) {
	if override := os.Getenv("ARIA_HOME"); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	legacy := filepath.Join(home, ".arthera")
	if _, err := os.Stat(legacy); err == nil {
		return legacy, nil
	}
	return filepath.Join(home, ".aria-code"), nil
}

// AriaSessionToken returns the token that `/login` stored for the signed-in
// Aria Code user. It is read on every call, never cached: the token lasts about
// an hour and `/login` refreshes it in the same file.
//
// This is for a service running on the user's own machine. A hosted service
// must not use it: it would be holding a user's login, and has no such file.
func AriaSessionToken() (string, error) {
	home, err := ariaHome()
	if err != nil {
		return "", errors.New("cannot locate Aria Code settings")
	}
	data, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		return "", errors.New("not signed in to Aria Code; run /login")
	}
	var config struct {
		AuthToken string `json:"auth_token"`
	}
	if json.Unmarshal(data, &config) != nil || config.AuthToken == "" {
		return "", errors.New("not signed in to Aria Code; run /login")
	}
	return config.AuthToken, nil
}
