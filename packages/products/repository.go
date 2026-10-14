// Package products owns the trusted merchant catalogue independently of HTTP or MCP.
package products

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/artheras/aria-plugins/packages/sdk"
	"github.com/artheras/aria-plugins/packages/store"
)

const Key = "meta_catalog"

var skuPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{1,63}$`)
var ErrStale = errors.New("catalogue changed; reload before saving")

type Snapshot struct {
	Products []sdk.Product `json:"products"`
	Digest   string        `json:"digest"`
}
type Repository struct {
	Store    store.Store
	Fallback []sdk.Product
}

func Validate(items []sdk.Product) error {
	if len(items) < 1 || len(items) > 50 {
		return errors.New("provide between 1 and 50 products")
	}
	seen := map[string]bool{}
	for _, p := range items {
		if !skuPattern.MatchString(p.SKU) || seen[p.SKU] || strings.TrimSpace(p.Name) == "" || len(p.Name) > 160 || p.Currency != "USD" || p.UnitAmount < 1 || p.UnitAmount > 1_000_000 {
			return errors.New("products require a unique SKU, a name, USD currency, and an integer price between 1 and 1000000 cents")
		}
		seen[p.SKU] = true
	}
	return nil
}
func snapshot(items []sdk.Product) Snapshot {
	copy := append([]sdk.Product(nil), items...)
	sort.Slice(copy, func(i, j int) bool { return copy[i].SKU < copy[j].SKU })
	raw, _ := json.Marshal(copy)
	h := sha256.Sum256(raw)
	return Snapshot{Products: copy, Digest: hex.EncodeToString(h[:])}
}
func (r Repository) load(ctx context.Context) (Snapshot, []byte, error) {
	raw, err := r.Store.Get(ctx, Key)
	if errors.Is(err, store.ErrNotFound) {
		if err = Validate(r.Fallback); err != nil {
			return Snapshot{}, nil, err
		}
		return snapshot(r.Fallback), nil, nil
	}
	if err != nil {
		return Snapshot{}, nil, err
	}
	var items []sdk.Product
	if json.Unmarshal(raw, &items) != nil || Validate(items) != nil {
		return Snapshot{}, nil, errors.New("stored catalogue is invalid")
	}
	return snapshot(items), raw, nil
}
func (r Repository) Snapshot(ctx context.Context) (Snapshot, error) {
	s, _, err := r.load(ctx)
	return s, err
}
func (r Repository) Products(ctx context.Context) ([]sdk.Product, error) {
	s, err := r.Snapshot(ctx)
	return s.Products, err
}
func (r Repository) Update(ctx context.Context, digest string, items []sdk.Product) (Snapshot, error) {
	if err := Validate(items); err != nil {
		return Snapshot{}, err
	}
	current, raw, err := r.load(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if digest == "" || digest != current.Digest {
		return Snapshot{}, ErrStale
	}
	next := snapshot(items)
	if next.Digest == current.Digest {
		return current, nil
	}
	data, _ := json.Marshal(next.Products)
	if err = r.Store.CAS(ctx, Key, raw, data); err != nil {
		return Snapshot{}, err
	}
	return next, nil
}
