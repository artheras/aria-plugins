package commerce

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/artheras/aria-plugins/packages/payments"
	"github.com/artheras/aria-plugins/packages/sdk"
)

type refundExternal struct {
	external
	refunds      int
	loseResponse bool
}

func (p *refundExternal) Refund(_ context.Context, i sdk.Invoice, capture, key string) (sdk.Refund, error) {
	p.refunds++
	p.saved.Payment.Status = "REFUNDED"
	if p.loseResponse {
		p.loseResponse = false
		return sdk.Refund{}, errors.New("lost refund response")
	}
	return sdk.Refund{ID: "REFUND1", CaptureID: capture, Status: "COMPLETED", Amount: i.Amount, Currency: i.Currency, Evidence: "verified_refund_resource"}, nil
}
func (p *refundExternal) InspectRefund(context.Context, string) (sdk.Refund, error) {
	return sdk.Refund{}, errors.New("not used")
}
func TestRefundBoundaryReplayAndUnknownOutcomeRecovery(t *testing.T) {
	for _, lost := range []bool{false, true} {
		s, _, base, now, o := fixture(t)
		p := &refundExternal{external: *base, loseResponse: lost}
		s.Payments = p
		ctx := context.Background()
		a := Actor{Owner: "owner", Human: true}
		o, err := s.Checkout(ctx, a, o.ID)
		if err != nil {
			t.Fatal(err)
		}
		o, err = s.Capture(ctx, a, o.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Refund(ctx, Actor{Owner: "owner"}, o.ID, o.Digest); err == nil {
			t.Fatal("agent refunded payment")
		}
		if _, err = s.Refund(ctx, a, o.ID, "wrong"); err == nil {
			t.Fatal("wrong invoice refunded")
		}
		o, err = s.Refund(ctx, a, o.ID, o.Digest)
		if lost {
			if err == nil {
				t.Fatal("uncertain outcome concealed")
			}
			*now = now.Add(3 * time.Minute)
			if _, _, _, err = s.Recover(ctx, "", 100); err != nil {
				t.Fatal(err)
			}
			o, err = s.Get(ctx, a, o.ID)
		}
		if err != nil || o.State != "refunded" || p.refunds != 1 || payments.ValidateRefund(o.Invoice, o.Payment.CaptureID, *o.Refund) != nil {
			t.Fatal(o, err, p.refunds)
		}
		if _, err = s.Refund(ctx, a, o.ID, o.Digest); err != nil || p.refunds != 1 {
			t.Fatal("refund replay repeated payment operation")
		}
	}
}
