package main

import (
	"context"
	"encoding/json"
	"golang.org/x/oauth2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentRefreshPersistsRotationWithoutReplay(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			t.Error("wrong endpoint")
		}
		r.ParseForm()
		if r.Form.Get("refresh_token") != "old-refresh" || r.Form.Get("client_id") != "public-client" {
			t.Error("wrong token binding")
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh-access", "refresh_token": "rotated-refresh", "token_type": "Bearer", "expires_in": 900})
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "private", "connection.json")
	old := saved{Base: srv.URL, Config: oauth2.Config{ClientID: "public-client", Endpoint: oauth2.Endpoint{AuthURL: srv.URL + "/oauth/authorize", TokenURL: srv.URL + "/oauth/token", AuthStyle: oauth2.AuthStyleInParams}}, Token: oauth2.Token{AccessToken: "expired", RefreshToken: "old-refresh", Expiry: time.Now().Add(-time.Hour)}}
	if e := save(path, old); e != nil {
		t.Fatal(e)
	}
	var group sync.WaitGroup
	for i := 0; i < 3; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			tok, e := (tokenSource{context.Background(), path, srv.URL}).Token()
			if e != nil || tok == nil || tok.AccessToken != "fresh-access" {
				t.Error("refresh failed", e)
			}
		}()
	}
	group.Wait()
	if calls.Load() != 1 {
		t.Fatal("refresh replayed", calls.Load())
	}
	restored, e := read(path, srv.URL)
	if e != nil || restored.Token.RefreshToken != "rotated-refresh" {
		t.Fatal("rotation not saved", e)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatal("credential permissions")
		}
		os.Chmod(path, 0644)
		if _, e = read(path, srv.URL); e == nil {
			t.Fatal("public credential file accepted")
		}
	}
}
func TestCredentialStoreRejectsCrossServiceAndSymlinks(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "credential.json")
	s := saved{Base: "https://aria.example", Config: oauth2.Config{Endpoint: oauth2.Endpoint{AuthURL: "https://aria.example/oauth/authorize", TokenURL: "https://evil.example/token"}}}
	if e := save(path, s); e != nil {
		t.Fatal(e)
	}
	if _, e := read(path, s.Base); e == nil {
		t.Fatal("credential forwarding allowed")
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(root, "link")
		if e := os.Symlink(path, link); e != nil {
			t.Fatal(e)
		}
		if _, e := read(link, s.Base); e == nil {
			t.Fatal("symlink accepted")
		}
	}
}
