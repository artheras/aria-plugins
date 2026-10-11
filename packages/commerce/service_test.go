package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/artheras/aria-plugins/packages/agents"
	"github.com/artheras/aria-plugins/packages/payments"
	"github.com/artheras/aria-plugins/packages/sdk"
	"github.com/artheras/aria-plugins/packages/store"
)

type flakyStore struct {
	store.Store
	failPaid, failIntent bool
}

func (f *flakyStore) CAS(ctx context.Context, key string, old, next []byte) error {
	var o sdk.Order
	_ = json.Unmarshal(next, &o)
	if f.failIntent && o.Operation != nil {
		return errors.New("disk unavailable")
	}
	if f.failPaid && o.State == "paid" {
		f.failPaid = false
		return errors.New("disk unavailable")
	}
	return f.Store.CAS(ctx, key, old, next)
}

type external struct {
	payments.Demo
	saved              payments.Snapshot
	captures, creates  int
	loseCreateResponse bool
}

func (p *external) Name() string { return "sandbox-test" }
func (p *external) Create(ctx context.Context, i sdk.Invoice, a, b string) (sdk.Payment, error) {
	p.creates++
	payment, _ := p.Demo.Create(ctx, i, a, b)
	if p.loseCreateResponse {
		p.loseCreateResponse = false
		return sdk.Payment{}, errors.New("response lost")
	}
	return payment, nil
}
func (p *external) Capture(ctx context.Context, i sdk.Invoice, id string) (sdk.Payment, error) {
	p.captures++
	result, _ := p.Demo.Capture(ctx, i, id)
	p.saved = payments.Snapshot{InvoiceID: i.ID, Payment: result}
	return result, nil
}
func (p *external) Inspect(context.Context, string) (payments.Snapshot, error) { return p.saved, nil }
func fixture(t *testing.T) (*Service, *flakyStore, *external, *time.Time, sdk.Order) {
	t.Helper()
	base, _ := store.NewFile(t.TempDir())
	f := &flakyStore{Store: base}
	p := &external{}
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	s := &Service{Store: f, Payments: p, Drafter: agents.Demo{}, PublicURL: "https://merchant.test", Now: func() time.Time { return now }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Catalog: []sdk.Product{{SKU: "TSH-001", Name: "Cotton tee", Currency: "USD", UnitAmount: 2400}, {SKU: "MUG-003", Name: "Ceramic mug", Currency: "USD", UnitAmount: 1200}}}
	a := Actor{Owner: "owner", Human: true}
	o, err := s.Draft(context.Background(), a, "2 tees and 1 mug")
	if err != nil {
		t.Fatal(err)
	}
	o, err = s.Approve(context.Background(), a, o.ID, o.Digest)
	if err != nil {
		t.Fatal(err)
	}
	return s, f, p, &now, o
}

// Wrap scanner explicitly, as embedding the Store interface does not expose Scan.
func (f *flakyStore) Scan(ctx context.Context, cursor string, limit int) ([]string, string, error) {
	return f.Store.(store.Scanner).Scan(ctx, cursor, limit)
}
func TestRecoveryAfterCaptureSucceededAndStorageFailed(t *testing.T) {
	s, f, p, now, o := fixture(t)
	ctx := context.Background()
	a := Actor{Owner: "owner", Human: true}
	o, err := s.Checkout(ctx, a, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.failPaid = true
	if _, err = s.Capture(ctx, a, o.ID); err == nil {
		t.Fatal("lost write not surfaced")
	}
	stored, _, _ := s.Load(ctx, o.ID, "owner")
	if stored.State != "awaiting_payment" || stored.Operation == nil {
		t.Fatal("durable intent lost")
	}
	*now = now.Add(3 * time.Minute)
	// Reconstruct the service to model a process restart; no in-memory task survives.
	restarted := *s
	_, count, _, err := restarted.Recover(ctx, "", 100)
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	stored, _, _ = s.Load(ctx, o.ID, "owner")
	if stored.State != "paid" || stored.Operation != nil || p.captures != 1 || len(stored.Events) != 4 {
		t.Fatal("recovery charged again or duplicated events", stored, p.captures)
	}
	_, count, _, err = restarted.Recover(ctx, "", 100)
	if err != nil || count != 0 || p.captures != 1 {
		t.Fatal("replay not idempotent")
	}
}
func TestIntentFailurePreventsExternalPayment(t *testing.T) {
	s, f, p, _, o := fixture(t)
	f.failIntent = true
	if _, err := s.Checkout(context.Background(), Actor{Owner: "owner", Human: true}, o.ID); err == nil {
		t.Fatal("missing storage failure")
	}
	if p.creates != 0 {
		t.Fatal("provider called before durable intent")
	}
}
func TestCheckoutRecoveryAndExpiredApproval(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "recover", true: "expired"}[expired], func(t *testing.T) {
			s, _, p, now, o := fixture(t)
			p.loseCreateResponse = true
			ctx := context.Background()
			if _, err := s.Checkout(ctx, Actor{Owner: "owner", Human: true}, o.ID); err == nil {
				t.Fatal("lost response not surfaced")
			}
			*now = now.Add(3 * time.Minute)
			if expired {
				*now = now.Add(time.Hour)
			}
			if _, _, _, err := s.Recover(ctx, "", 100); err != nil {
				t.Fatal(err)
			}
			stored, _, _ := s.Load(ctx, o.ID, "owner")
			if expired {
				if p.creates != 1 || stored.Operation == nil || !stored.Operation.NeedsReview {
					t.Fatal("expired approval retried")
				}
			} else if p.creates != 2 || stored.State != "awaiting_payment" || stored.Operation != nil {
				t.Fatal("checkout not recovered")
			}
		})
	}
}
func TestRecoveryNeverCapturesUnpaidSandboxOrder(t *testing.T) {
	s, _, p, now, o := fixture(t)
	ctx := context.Background()
	o, err := s.Checkout(ctx, Actor{Owner: "owner", Human: true}, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, raw, _ := s.Load(ctx, o.ID, "owner")
	stored.Operation = &sdk.Operation{Kind: "capture", ID: "operation", NextAttemptAt: now.Format(time.RFC3339), StartedAt: now.Format(time.RFC3339)}
	s.Save(ctx, stored, raw)
	p.saved = payments.Snapshot{InvoiceID: o.ID, Payment: *o.Payment}
	p.saved.Payment.Status = "APPROVED"
	s.Recover(ctx, "", 100)
	stored, _, _ = s.Load(ctx, o.ID, "owner")
	if p.captures != 0 || stored.State != "awaiting_payment" || stored.Operation != nil {
		t.Fatal("worker captured without a new human action")
	}
}
func TestServiceRejectsAgentPaymentActions(t *testing.T) {
	s, _, _, _, o := fixture(t)
	a := Actor{Owner: "owner"}
	ctx := context.Background()
	for _, call := range []func() error{func() error { _, e := s.Approve(ctx, a, o.ID, o.Digest); return e }, func() error { _, e := s.Checkout(ctx, a, o.ID); return e }, func() error { _, e := s.Capture(ctx, a, o.ID); return e }} {
		var e *Error
		if !errors.As(call(), &e) || e.Status != 403 {
			t.Fatal("agent crossed human boundary")
		}
	}
}

func TestServiceDraftValidatesPromptBeforeProvider(t *testing.T) {
	s, _, _, _, _ := fixture(t)
	_, err := s.Draft(context.Background(), Actor{Owner: "owner"}, strings.Repeat("x", 2001))
	var problem *Error
	if !errors.As(err, &problem) || problem.Code != "invalid_prompt" {
		t.Fatal("oversized input accepted", err)
	}
}
