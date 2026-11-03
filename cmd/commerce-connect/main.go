// commerce-connect connects stdio-only hosts to the remote Commerce MCP service.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

const defaultURL = "https://aria-commerce-sandbox-vrkp6bxpya-nw.a.run.app"

type saved struct {
	Base   string
	Config oauth2.Config
	Token  oauth2.Token
}

func read(path, base string) (saved, error) {
	var s saved
	info, e := os.Lstat(path)
	if e != nil {
		return s, errors.New("run commerce-connect login first")
	}
	if !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return s, errors.New("connection file must be a regular file accessible only by its owner")
	}
	raw, e := os.ReadFile(path)
	if e != nil || json.Unmarshal(raw, &s) != nil || s.Base != base || s.Config.Endpoint.TokenURL != base+"/oauth/token" || s.Config.Endpoint.AuthURL != base+"/oauth/authorize" {
		return s, errors.New("invalid saved connection")
	}
	return s, nil
}
func save(path string, s saved) error {
	dir := filepath.Dir(path)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".oauth-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	raw, e := json.Marshal(s)
	if e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(name, path)
}
func lock(ctx context.Context, path string) (func(), error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	for i := 0; i < 200; i++ {
		e := os.Mkdir(path+".lock", 0700)
		if e == nil {
			return func() { _ = os.Remove(path + ".lock") }, nil
		}
		if !os.IsExist(e) {
			return nil, e
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return nil, errors.New("connection is in use; if a process crashed, remove its stale .lock directory after closing clients")
}

type tokenSource struct {
	ctx        context.Context
	path, base string
}

func (t tokenSource) Token() (*oauth2.Token, error) {
	unlock, e := lock(t.ctx, t.path)
	if e != nil {
		return nil, e
	}
	defer unlock()
	s, e := read(t.path, t.base)
	if e != nil {
		return nil, e
	}
	tok, e := s.Config.TokenSource(t.ctx, &s.Token).Token()
	if e != nil {
		return nil, errors.New("connection renewal failed; run commerce-connect login again")
	}
	if tok.AccessToken != s.Token.AccessToken {
		s.Token = *tok
		if e = save(t.path, s); e != nil {
			return nil, errors.New("cannot securely save renewed connection")
		}
	}
	return tok, nil
}
func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}
func login(ctx context.Context, path, base string, noBrowser bool) error {
	unlock, e := lock(ctx, path)
	if e != nil {
		return e
	}
	defer unlock()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return errors.New("cannot start the loopback login callback")
	}
	defer listener.Close()
	redirect := "http://" + listener.Addr().String() + "/callback"
	// Only the fixed Commerce origin is used. Never discover or fetch arbitrary
	// authorization endpoints or forward a credential to another host.
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	raw, _ := json.Marshal(map[string]any{"client_name": "Aria Commerce local connector", "redirect_uris": []string{redirect}, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}})
	req, _ := http.NewRequestWithContext(ctx, "POST", base+"/oauth/register", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	res, e := client.Do(req)
	if e != nil {
		return errors.New("cannot register this client")
	}
	var registered struct {
		ID string `json:"client_id"`
	}
	if res.StatusCode != 201 {
		res.Body.Close()
		return errors.New("client registration was rejected")
	}
	e = json.NewDecoder(io.LimitReader(res.Body, 16<<10)).Decode(&registered)
	res.Body.Close()
	if e != nil || registered.ID == "" {
		return errors.New("invalid registration response")
	}
	cfg := oauth2.Config{ClientID: registered.ID, RedirectURL: redirect, Scopes: []string{"read", "draft", "offline_access"}, Endpoint: oauth2.Endpoint{AuthURL: base + "/oauth/authorize", TokenURL: base + "/oauth/token", AuthStyle: oauth2.AuthStyleInParams}}
	state, proof := oauth2.GenerateVerifier(), oauth2.GenerateVerifier()
	target := cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(proof), oauth2.SetAuthURLParam("resource", base+"/mcp"))
	result := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.URL.Query().Get("state") != state || r.URL.Query().Get("iss") != base || r.URL.Query().Get("code") == "" {
			http.Error(w, "Authorization was not completed. Return to your terminal.", 400)
			return
		}
		select {
		case result <- r.URL.Query().Get("code"):
			io.WriteString(w, "Connected. You can close this window and return to Aria.")
		default:
			http.Error(w, "This callback has already been used.", 400)
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	if noBrowser || openBrowser(target) != nil {
		fmt.Fprintln(os.Stderr, "Open this address in your browser to sign in and review access:\n"+target)
	} else {
		fmt.Fprintln(os.Stderr, "Browser opened. Sign in, review the app and permissions, then choose Allow connection.")
	}
	select {
	case code := <-result:
		tok, e := cfg.Exchange(ctx, code, oauth2.VerifierOption(proof), oauth2.SetAuthURLParam("resource", base+"/mcp"))
		if e != nil {
			return errors.New("authorization exchange failed")
		}
		if e = save(path, saved{Base: base, Config: cfg, Token: *tok}); e != nil {
			return errors.New("cannot securely save connection")
		}
		fmt.Fprintln(os.Stderr, "Connected. Your credential is saved locally outside the chat.")
		return nil
	case <-ctx.Done():
		return errors.New("login timed out or was cancelled")
	}
}
func serve(ctx context.Context, path, base string) error {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
	client := mcp.NewClient(&mcp.Implementation{Name: "aria-commerce-connector", Version: "1.0.0"}, nil)
	httpClient := oauth2.NewClient(ctx, tokenSource{ctx, path, base})
	httpClient.Timeout = 60 * time.Second
	session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: base + "/mcp", HTTPClient: httpClient}, nil)
	if e != nil {
		return errors.New("remote connection failed; run commerce-connect login to reconnect")
	}
	defer session.Close()
	tools, e := session.ListTools(ctx, nil)
	if e != nil {
		return errors.New("cannot list remote tools")
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "aria-commerce", Version: "1.0.0"}, nil)
	allowed := map[string]bool{"commerce_profile": true, "commerce_catalog": true, "commerce_get_order": true, "commerce_draft_order": true}
	for _, tool := range tools.Tools {
		if !allowed[tool.Name] {
			continue
		}
		name := tool.Name
		server.AddTool(tool, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			out, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: r.Params.Arguments})
			if e != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "Remote call failed. Do not automatically retry a draft; check your account for an existing order first."}}}, nil
			}
			return out, nil
		})
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: commerce-connect login|status|logout|serve [--url URL] [--store PATH]")
	}
	flags := flag.NewFlagSet("commerce-connect", flag.ContinueOnError)
	base := flags.String("url", defaultURL, "canonical Commerce origin")
	root, e := os.UserConfigDir()
	if e != nil {
		return e
	}
	path := flags.String("store", filepath.Join(root, "aria-plugins", "commerce-oauth.json"), "private local credential file")
	noBrowser := flags.Bool("no-browser", false, "print the authorization URL instead of opening a browser")
	if e = flags.Parse(args[1:]); e != nil {
		return e
	}
	u, e := url.Parse(*base)
	if e != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && u.Hostname() == "127.0.0.1")) {
		return errors.New("provide a canonical HTTPS origin, or loopback HTTP for development")
	}
	if !filepath.IsAbs(*path) {
		return errors.New("credential store path must be absolute")
	}
	ctx := context.Background()
	switch args[0] {
	case "login":
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		return login(ctx, *path, *base, *noBrowser)
	case "serve":
		return serve(ctx, *path, *base)
	case "status":
		s, e := read(*path, *base)
		if e != nil {
			return e
		}
		fmt.Fprintf(os.Stdout, "Connected to %s\nAccess expires: %s\nRenewal available: %t\nServer revocation is checked on the next request.\n", s.Base, s.Token.Expiry.Format(time.RFC3339), s.Token.RefreshToken != "")
		return nil
	case "logout":
		unlock, e := lock(ctx, *path)
		if e != nil {
			return e
		}
		defer unlock()
		s, e := read(*path, *base)
		if e != nil {
			return e
		}
		token := s.Token.RefreshToken
		if token == "" {
			token = s.Token.AccessToken
		}
		client := http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, e := client.PostForm(*base+"/oauth/revoke", url.Values{"token": {token}, "client_id": {s.Config.ClientID}})
		if e != nil {
			return errors.New("cannot revoke remotely; revoke in your Commerce account and retry")
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			return errors.New("remote revocation failed; saved connection retained")
		}
		if e = os.Remove(*path); e != nil {
			return e
		}
		fmt.Fprintln(os.Stdout, "Disconnected and saved credential removed.")
		return nil
	default:
		return errors.New("unknown command")
	}
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
