// Package limits enforces shared, persistent quotas before expensive work.
package limits

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/artheras/aria-plugins/packages/store"
)

var ErrLimited = errors.New("request limit reached")

type Limiter struct {
	Store store.Store
	Now   func() time.Time
}
type bucket struct {
	Window int64 `json:"window"`
	Count  int   `json:"count"`
}

// Take uses stable hashed keys: rotating windows never creates extra records.
func (l Limiter) Take(ctx context.Context, scope, subject string, maximum int, window time.Duration) (int, error) {
	now := time.Now()
	if l.Now != nil {
		now = l.Now()
	}
	duration := int64(window / time.Second)
	if maximum < 1 || duration < 1 {
		return 0, errors.New("invalid quota configuration")
	}
	current := now.Unix() / duration
	retry := int((current+1)*duration - now.Unix())
	h := sha256.Sum256([]byte(scope + "\x00" + subject))
	key := "rl_" + hex.EncodeToString(h[:])
	for range 12 {
		raw, err := l.Store.Get(ctx, key)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return 0, err
		}
		var b bucket
		if len(raw) > 0 && json.Unmarshal(raw, &b) != nil {
			return 0, errors.New("invalid quota record")
		}
		if b.Window != current {
			b = bucket{Window: current}
		}
		if b.Count >= maximum {
			return retry, ErrLimited
		}
		b.Count++
		next, _ := json.Marshal(b)
		err = l.Store.CAS(ctx, key, raw, next)
		if errors.Is(err, store.ErrConflict) {
			continue
		}
		return 0, err
	}
	return 0, store.ErrConflict
}
