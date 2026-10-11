// Package store offers atomic compare-and-swap persistence for plugin records.
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("record not found")
var ErrConflict = errors.New("record changed; reload and retry")

type Store interface {
	Get(context.Context, string) ([]byte, error)
	CAS(context.Context, string, []byte, []byte) error
}

var validKey = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)

// File is for one server process. Use Redis for multiple instances/serverless.
type File struct {
	Dir string
	mu  sync.Mutex
}

func NewFile(dir string) (*File, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &File{Dir: dir}, nil
}
func (f *File) Get(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.read(key)
}
func (f *File) read(key string) ([]byte, error) {
	if !validKey.MatchString(key) {
		return nil, ErrNotFound
	}
	b, err := os.ReadFile(filepath.Join(f.Dir, key+".json"))
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	return b, err
}
func (f *File) CAS(_ context.Context, key string, expected, next []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !validKey.MatchString(key) {
		return errors.New("invalid record key")
	}
	current, err := f.read(key)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if !bytes.Equal(current, expected) {
		return ErrConflict
	}
	tmp, err := os.CreateTemp(f.Dir, ".record-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(next); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, filepath.Join(f.Dir, key+".json")); err != nil {
		return err
	}
	dir, err := os.Open(f.Dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

type Redis struct {
	URL, Token, Prefix string
	Client             *http.Client
}

func (r Redis) command(ctx context.Context, args []any) (json.RawMessage, error) {
	body, _ := json.Marshal(args)
	req, err := http.NewRequestWithContext(ctx, "POST", r.URL, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("invalid Redis endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+r.Token)
	req.Header.Set("Content-Type", "application/json")
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("storage unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, errors.New("storage request rejected")
	}
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&out); err != nil || out.Error != "" {
		return nil, errors.New("invalid storage response")
	}
	return out.Result, nil
}
func (r Redis) key(key string) (string, error) {
	if !validKey.MatchString(key) {
		return "", ErrNotFound
	}
	prefix := r.Prefix
	if prefix == "" {
		prefix = "aria:commerce:"
	}
	return prefix + key, nil
}
func (r Redis) Get(ctx context.Context, key string) ([]byte, error) {
	k, err := r.key(key)
	if err != nil {
		return nil, err
	}
	result, err := r.command(ctx, []any{"GET", k})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(result)) == "null" {
		return nil, ErrNotFound
	}
	var value string
	if json.Unmarshal(result, &value) != nil {
		return nil, errors.New("invalid storage record")
	}
	return []byte(value), nil
}
func (r Redis) CAS(ctx context.Context, key string, expected, next []byte) error {
	k, err := r.key(key)
	if err != nil {
		return err
	}
	script := `local v=redis.call('GET',KEYS[1]); if (not v and ARGV[1]=='') or v==ARGV[1] then redis.call('SET',KEYS[1],ARGV[2]); return 1 else return 0 end`
	result, err := r.command(ctx, []any{"EVAL", script, 1, k, string(expected), string(next)})
	if err != nil {
		return err
	}
	if string(result) != "1" {
		return ErrConflict
	}
	return nil
}
