package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
)

// Scanner pages durable records for recovery without a separate volatile queue.
type Scanner interface {
	Scan(context.Context, string, int) ([]string, string, error)
}

func (f *File) Scan(ctx context.Context, cursor string, limit int) ([]string, string, error) {
	if limit < 1 || limit > 500 {
		return nil, "", errors.New("scan limit must be between 1 and 500")
	}
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	entries, err := os.ReadDir(f.Dir)
	if err != nil {
		return nil, "", err
	}
	var keys []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		key := strings.TrimSuffix(e.Name(), ".json")
		if validKey.MatchString(key) && !strings.HasPrefix(key, "rl_") && key > cursor {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	next := ""
	if len(keys) > limit {
		keys = keys[:limit]
		next = keys[len(keys)-1]
	}
	return keys, next, nil
}
func (r Redis) Scan(ctx context.Context, cursor string, limit int) ([]string, string, error) {
	if limit < 1 || limit > 500 {
		return nil, "", errors.New("scan limit must be between 1 and 500")
	}
	if cursor == "" {
		cursor = "0"
	}
	prefix, _ := r.key("scan")
	prefix = strings.TrimSuffix(prefix, "scan")
	data, err := r.command(ctx, []any{"SCAN", cursor, "MATCH", prefix + "*", "COUNT", limit})
	if err != nil {
		return nil, "", err
	}
	var result []json.RawMessage
	if json.Unmarshal(data, &result) != nil || len(result) != 2 {
		return nil, "", errors.New("invalid scan response")
	}
	var next string
	var all []string
	if json.Unmarshal(result[0], &next) != nil || json.Unmarshal(result[1], &all) != nil {
		return nil, "", errors.New("invalid scan response")
	}
	var keys []string
	for _, full := range all {
		if !strings.HasPrefix(full, prefix) {
			return nil, "", errors.New("unexpected scan prefix")
		}
		key := strings.TrimPrefix(full, prefix)
		if validKey.MatchString(key) && !strings.HasPrefix(key, "rl_") {
			keys = append(keys, key)
		}
	}
	if next == "0" {
		next = ""
	}
	return keys, next, nil
}
