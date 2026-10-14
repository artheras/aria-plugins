package products

import (
	"context"
	"errors"
	"testing"

	"github.com/artheras/aria-plugins/packages/sdk"
	"github.com/artheras/aria-plugins/packages/store"
)

func TestCatalogueCASAndPersistence(t *testing.T) {
	ctx := context.Background()
	base, _ := store.NewFile(t.TempDir())
	r := Repository{Store: base, Fallback: []sdk.Product{{SKU: "TEE-01", Name: "Tee", Currency: "USD", UnitAmount: 2400}}}
	initial, _ := r.Snapshot(ctx)
	updated, err := r.Update(ctx, initial.Digest, []sdk.Product{{SKU: "TEE-01", Name: "Tee", Currency: "USD", UnitAmount: 2500}})
	if err != nil || updated.Digest == initial.Digest {
		t.Fatal(updated, err)
	}
	if _, err = r.Update(ctx, initial.Digest, initial.Products); !errors.Is(err, ErrStale) {
		t.Fatal("stale catalogue accepted", err)
	}
	fresh, err := r.Snapshot(ctx)
	if err != nil || fresh.Products[0].UnitAmount != 2500 {
		t.Fatal("catalogue not persisted")
	}
	if Validate([]sdk.Product{{SKU: "TEE-01", Name: "Tee", Currency: "USD", UnitAmount: -1}}) == nil {
		t.Fatal("unsafe price accepted")
	}
}
