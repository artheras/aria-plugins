package commerce

import (
	"context"
	"testing"
	"time"

	"github.com/artheras/aria-plugins/packages/sdk"
)

func TestReviseRequiresNewApprovalAndCannotChangeCheckout(t *testing.T) {
	s, _, _, _, o := fixture(t)
	ctx := context.Background()
	a := Actor{Owner: "owner", Human: true}
	oldDigest := o.Digest
	o, err := s.Revise(ctx, a, o.ID, o.Digest, []sdk.Selection{{SKU: "MUG-003", Quantity: 2}})
	if err != nil || o.State != "draft" || o.Amount != 2400 || o.ApprovedDigest != "" || o.Digest == oldDigest {
		t.Fatal(o, err)
	}
	if _, err = s.Checkout(ctx, a, o.ID); err == nil {
		t.Fatal("revised invoice reused previous approval")
	}
	if _, err = s.Approve(ctx, a, o.ID, oldDigest); err == nil {
		t.Fatal("old digest approved new invoice")
	}
	o, err = s.Approve(ctx, a, o.ID, o.Digest)
	if err != nil {
		t.Fatal(err)
	}
	o, err = s.Checkout(ctx, a, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Revise(ctx, a, o.ID, o.Digest, []sdk.Selection{{SKU: "MUG-003", Quantity: 1}}); err == nil {
		t.Fatal("existing checkout revised")
	}
	if _, err = s.Cancel(ctx, a, o.ID, o.Digest); err == nil {
		t.Fatal("existing checkout cancelled locally")
	}
}
func TestCancelExpiryAndAgentBoundary(t *testing.T) {
	s, _, _, now, o := fixture(t)
	ctx := context.Background()
	humanActor := Actor{Owner: "owner", Human: true}
	if _, err := s.Cancel(ctx, Actor{Owner: "owner"}, o.ID, o.Digest); err == nil {
		t.Fatal("agent cancelled order")
	}
	cancelled, err := s.Cancel(ctx, humanActor, o.ID, o.Digest)
	if err != nil || cancelled.State != "cancelled" {
		t.Fatal(cancelled, err)
	}
	replay, err := s.Cancel(ctx, humanActor, o.ID, o.Digest)
	if err != nil || len(replay.Events) != len(cancelled.Events) {
		t.Fatal("duplicate cancellation event")
	}
	o, err = s.Draft(ctx, humanActor, "1 mug")
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(25 * time.Hour)
	if _, err = s.Approve(ctx, humanActor, o.ID, o.Digest); err == nil {
		t.Fatal("expired draft approved")
	}
	o, err = s.Get(ctx, humanActor, o.ID)
	if err != nil || o.State != "expired" {
		t.Fatal(o, err)
	}
}
func TestListOwnershipAndPagination(t *testing.T) {
	s, _, _, _, first := fixture(t)
	ctx := context.Background()
	a := Actor{Owner: "owner", Human: true}
	for range 4 {
		if _, err := s.Draft(ctx, a, "1 mug"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Draft(ctx, Actor{Owner: "other"}, "1 mug"); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	cursor := ""
	for range 10 {
		page, err := s.List(ctx, a, ListOptions{Cursor: cursor, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range page.Orders {
			if o.Owner != "owner" || seen[o.ID] {
				t.Fatal("pagination leaked or repeated a record")
			}
			seen[o.ID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 5 || !seen[first.ID] || cursor != "" {
		t.Fatal("pagination lost records", seen)
	}
}
func TestResumeRetainsIntentAndNeedsHumanReview(t *testing.T) {
	s, _, p, now, o := fixture(t)
	ctx := context.Background()
	a := Actor{Owner: "owner", Human: true}
	p.loseCreateResponse = true
	s.Checkout(ctx, a, o.ID)
	*now = now.Add(time.Hour)
	s.Recover(ctx, "", 100)
	o, _, _ = s.Load(ctx, o.ID, a.Owner)
	operationID := o.Operation.ID
	if _, err := s.Resume(ctx, Actor{Owner: "owner"}, o.ID, o.Digest); err == nil {
		t.Fatal("agent resumed payment intent")
	}
	o, err := s.Resume(ctx, a, o.ID, o.Digest)
	if err != nil || o.Operation.ID != operationID || o.Operation.NeedsReview {
		t.Fatal(o, err)
	}
	if _, _, _, err = s.Recover(ctx, "", 100); err != nil {
		t.Fatal(err)
	}
	o, _, _ = s.Load(ctx, o.ID, a.Owner)
	if o.State != "awaiting_payment" || p.captures != 0 {
		t.Fatal("resume charged or failed")
	}
}
func TestOwnerMigrationIsPreviewFirstAndPreservesInvoice(t *testing.T) {
	s, _, _, _, o := fixture(t)
	ctx := context.Background()
	_, count, err := MigrateOwner(ctx, s.Store, "owner", "merchant_store-1", "", 100, false)
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err = s.Get(ctx, Actor{Owner: "merchant_store-1"}, o.ID); err == nil {
		t.Fatal("preview changed owner")
	}
	_, count, err = MigrateOwner(ctx, s.Store, "owner", "merchant_store-1", "", 100, true)
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	moved, err := s.Get(ctx, Actor{Owner: "merchant_store-1"}, o.ID)
	if err != nil || moved.Digest != o.Digest || moved.Amount != o.Amount {
		t.Fatal("migration changed invoice")
	}
}
