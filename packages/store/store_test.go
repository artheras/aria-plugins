package store

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestFileDurabilityAndConcurrentCAS(t *testing.T) {
	dir := t.TempDir()
	f, err := NewFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if f.CAS(ctx, "order", nil, []byte("initial")) != nil {
		t.Fatal("initial write failed")
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if f.CAS(ctx, "order", []byte("initial"), []byte("next")) == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("compare-and-swap allowed multiple writers")
	}
	reopened, _ := NewFile(dir)
	value, err := reopened.Get(ctx, "order")
	if err != nil || string(value) != "next" {
		t.Fatal("record did not survive reopening")
	}
	if _, err = f.Get(ctx, "../private"); !errors.Is(err, ErrNotFound) {
		t.Fatal("unsafe storage key accepted")
	}
}
func TestRedisConflictAndUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Error("missing storage authentication")
		}
		w.Write([]byte(`{"result":0}`))
	}))
	defer server.Close()
	redis := Redis{URL: server.URL, Token: "test"}
	if err := redis.CAS(context.Background(), "id", nil, []byte("{}")); !errors.Is(err, ErrConflict) {
		t.Fatal("Redis conflict not surfaced", err)
	}
	server.Close()
	if _, err := redis.Get(context.Background(), "id"); err == nil {
		t.Fatal("storage failure hidden")
	}
}
