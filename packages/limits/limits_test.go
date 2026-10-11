package limits

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/artheras/aria-plugins/packages/store"
)

func TestSharedQuotaConcurrencyAndWindowReset(t *testing.T) {
	storage, _ := store.NewFile(t.TempDir())
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	l := Limiter{Store: storage, Now: func() time.Time { return now }}
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := l.Take(context.Background(), "draft", "owner", 5, time.Minute)
			if err == nil {
				allowed.Add(1)
			} else if !errors.Is(err, ErrLimited) && !errors.Is(err, store.ErrConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 5 {
		t.Fatal("concurrent quota exceeded", allowed.Load())
	}
	second := Limiter{Store: storage, Now: l.Now}
	retry, err := second.Take(context.Background(), "draft", "owner", 5, time.Minute)
	if !errors.Is(err, ErrLimited) || retry != 60 {
		t.Fatal("second instance bypassed limit", retry, err)
	}
	now = now.Add(time.Minute)
	if _, err = second.Take(context.Background(), "draft", "owner", 5, time.Minute); err != nil {
		t.Fatal("window not reset", err)
	}
}
