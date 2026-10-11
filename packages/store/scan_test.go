package store

import (
	"context"
	"testing"
)

func TestFileRecoveryScanPaginationExcludesQuotaRecords(t *testing.T) {
	f, _ := NewFile(t.TempDir())
	ctx := context.Background()
	for _, key := range []string{"c", "a", "b", "rl_limit"} {
		if err := f.CAS(ctx, key, nil, []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	keys, next, err := f.Scan(ctx, "", 2)
	if err != nil || len(keys) != 2 || keys[0] != "a" || keys[1] != "b" || next != "b" {
		t.Fatal(keys, next, err)
	}
	keys, next, err = f.Scan(ctx, next, 2)
	if err != nil || len(keys) != 1 || keys[0] != "c" || next != "" {
		t.Fatal(keys, next, err)
	}
}
