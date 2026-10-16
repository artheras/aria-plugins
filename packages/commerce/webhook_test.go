package commerce

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/artheras/aria-plugins/packages/payments"
)

// hooked lets a test choose what provider verification returns.
type hooked struct {
	*external
	hook      payments.Webhook
	verifyErr error
	inspectEr error
}

func (h *hooked) Verify(context.Context, http.Header, []byte) (payments.Webhook, error) {
	return h.hook, h.verifyErr
}
func (h *hooked) Inspect(ctx context.Context, id string) (payments.Snapshot, error) {
	if h.inspectEr != nil {
		return payments.Snapshot{}, h.inspectEr
	}
	return h.external.Inspect(ctx, id)
}

func codeOf(t *testing.T, err error) (string, int, bool) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("not a commerce error: %v", err)
	}
	return e.Code, e.Status, e.Retryable
}

// A webhook is money-critical: the provider may deliver it twice, out of order,
// forged, or while we cannot reach the provider. Each case must leave the order
// in a state a retry can still repair.
func TestWebhookReconcilesOnceAndIgnoresReplay(t *testing.T) {
	s, _, p, _, o := fixture(t)
	ctx := context.Background()
	a := Actor{Owner: "owner", Human: true}
	o, err := s.Checkout(ctx, a, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The provider captured the payment but our own capture call never ran.
	if _, err = p.Capture(ctx, o.Invoice, o.Payment.OrderID); err != nil {
		t.Fatal(err)
	}
	h := &hooked{external: p, hook: payments.Webhook{ID: "evt-1", OrderID: o.Payment.OrderID}}
	s.Payments = h

	got, err := s.Webhook(ctx, http.Header{}, []byte("{}"))
	if err != nil || got != "reconciled" {
		t.Fatal(got, err)
	}
	stored, _, _ := s.Load(ctx, o.ID, "owner")
	if stored.State != "paid" {
		t.Fatalf("order not paid after verified webhook: %s", stored.State)
	}
	events := len(stored.Events)

	got, err = s.Webhook(ctx, http.Header{}, []byte("{}"))
	if err != nil || got != "duplicate" {
		t.Fatalf("replay not recognised: %q %v", got, err)
	}
	stored, _, _ = s.Load(ctx, o.ID, "owner")
	if len(stored.Events) != events || p.captures != 1 {
		t.Fatal("replay changed the order or captured again", len(stored.Events), events, p.captures)
	}
}

func TestWebhookRejectsForgedMismatchedAndUnreachable(t *testing.T) {
	s, _, p, _, o := fixture(t)
	ctx := context.Background()
	a := Actor{Owner: "owner", Human: true}
	o, err := s.Checkout(ctx, a, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Capture(ctx, o.Invoice, o.Payment.OrderID); err != nil {
		t.Fatal(err)
	}
	h := &hooked{external: p}
	s.Payments = h

	h.verifyErr = errors.New("bad signature")
	_, err = s.Webhook(ctx, http.Header{}, []byte("{}"))
	if code, status, retry := codeOf(t, err); code != "invalid_webhook" || status != 400 || retry {
		t.Fatal("forged webhook not rejected", code, status, retry)
	}

	h.verifyErr = nil
	h.hook = payments.Webhook{} // verified but about no order
	if got, err := s.Webhook(ctx, http.Header{}, []byte("{}")); err != nil || got != "ignored" {
		t.Fatal(got, err)
	}

	h.hook = payments.Webhook{ID: "evt-x", OrderID: "someone-elses-order"}
	_, err = s.Webhook(ctx, http.Header{}, []byte("{}"))
	if code, status, _ := codeOf(t, err); code != "webhook_mismatch" || status != 409 {
		t.Fatal("webhook for a different provider order accepted", code, status)
	}

	h.hook = payments.Webhook{ID: "evt-y", OrderID: o.Payment.OrderID}
	h.inspectEr = errors.New("provider down")
	_, err = s.Webhook(ctx, http.Header{}, []byte("{}"))
	if code, status, retry := codeOf(t, err); code != "provider_unavailable" || status != 503 || !retry {
		t.Fatal("provider outage must ask the sender to retry", code, status, retry)
	}
	stored, _, _ := s.Load(ctx, o.ID, "owner")
	if stored.State == "paid" {
		t.Fatal("order marked paid by a rejected webhook")
	}
}
