package commerce

import (
	"context"
	"time"

	"github.com/artheras/aria-plugins/packages/approvals"
	"github.com/artheras/aria-plugins/packages/payments"
	"github.com/artheras/aria-plugins/packages/sdk"
)

func (s *Service) finishRefund(ctx context.Context, o sdk.Order, raw []byte, r sdk.Refund) (sdk.Order, error) {
	if o.Payment == nil || payments.ValidateRefund(o.Invoice, o.Payment.CaptureID, r) != nil {
		return o, problem("refund_mismatch", "Refund does not match the captured invoice.", 502, false)
	}
	if o.State != "refunded" {
		o.Refund = &r
		o.State = "refunded"
		o.Operation = nil
		s.event(&o, "refund_reconciled", "Full refund matched the original capture, amount and currency")
	}
	return o, s.Save(ctx, o, raw)
}
func (s *Service) inspectPayment(ctx context.Context, o sdk.Order) (sdk.Payment, error) {
	snap, err := s.Payments.Inspect(ctx, o.Payment.OrderID)
	if err != nil {
		return sdk.Payment{}, problem("provider_unavailable", "Provider status unavailable. Keep the same order.", 503, true)
	}
	if snap.InvoiceID != o.ID || snap.Payment.OrderID != o.Payment.OrderID || snap.Payment.CaptureID != o.Payment.CaptureID || snap.Payment.Amount != o.Amount || snap.Payment.Currency != o.Currency {
		return sdk.Payment{}, problem("payment_mismatch", "Provider status does not match the original captured invoice.", 502, false)
	}
	return snap.Payment, nil
}

// Refund requires a fresh human request and exact digest; only full refunds are
// supported. Recovery may inspect a refund, but cannot initiate or repeat one.
func (s *Service) Refund(ctx context.Context, a Actor, id, digest string) (sdk.Order, error) {
	if err := human(a); err != nil {
		return sdk.Order{}, err
	}
	o, raw, err := s.Load(ctx, id, a.Owner)
	if err != nil {
		return o, err
	}
	if digest == "" || digest != o.Digest || digest != approvals.Digest(o.Invoice) {
		return o, problem("invoice_changed", "Review the exact captured invoice before refunding.", 409, false)
	}
	if o.State == "refunded" {
		return o, nil
	}
	if (o.State != "paid" && o.State != "refund_pending") || o.Payment == nil || o.Payment.CaptureID == "" || o.ApprovedDigest != digest || (o.Operation != nil && o.Operation.Kind != "refund") {
		return o, problem("refund_unavailable", "Only an intact, captured order can be refunded.", 409, false)
	}
	provider, ok := s.Payments.(payments.Refunder)
	if !ok {
		return o, problem("refund_unavailable", "This provider does not support refunds.", 422, false)
	}
	if s.Payments.Name() != "demo" {
		remote, err := s.inspectPayment(ctx, o)
		if err != nil {
			return o, err
		}
		if remote.Status == "REFUNDED" {
			return s.finishRefund(ctx, o, raw, sdk.Refund{CaptureID: remote.CaptureID, Status: "COMPLETED", Amount: o.Amount, Currency: o.Currency, Evidence: "verified_capture_refunded_status"})
		}
		if remote.Status != "COMPLETED" {
			return o, problem("refund_requires_review", "Provider status requires review. Do not issue another refund.", 409, false)
		}
	}
	if o.Operation == nil {
		raw, err = s.intent(ctx, &o, raw, "refund")
		if err != nil {
			return o, err
		}
	} else {
		// Never replay a monetary request automatically, or after an unbounded
		// provider idempotency window. Unknown outcomes require provider review.
		if o.Refund == nil || o.Refund.ID == "" {
			return o, problem("refund_requires_review", "Refund outcome is uncertain. Refresh provider status or review it in PayPal before retrying.", 409, false)
		}
	}
	var result sdk.Refund
	if o.Refund != nil && o.Refund.ID != "" {
		result, err = provider.InspectRefund(ctx, o.Refund.ID)
	} else {
		result, err = provider.Refund(ctx, o.Invoice, o.Payment.CaptureID, o.Operation.ID)
	}
	if err != nil {
		return o, s.operationFailure(ctx, o, raw, "refund_pending")
	}
	if result.CaptureID != o.Payment.CaptureID || result.Amount != o.Amount || result.Currency != o.Currency || result.ID == "" {
		return o, s.operationFailure(ctx, o, raw, "refund_mismatch")
	}
	if result.Status == "COMPLETED" {
		return s.finishRefund(ctx, o, raw, result)
	}
	o.Refund = &result
	o.State = "refund_pending"
	o.Operation.NextAttemptAt = s.now().Add(2 * time.Minute).Format(time.RFC3339)
	if result.Status != "PENDING" {
		o.Operation.NeedsReview = true
		o.Operation.LastCode = "refund_failed"
	}
	if err = s.Save(ctx, o, raw); err != nil {
		return o, err
	}
	return o, nil
}
func (s *Service) recoverRefund(ctx context.Context, o sdk.Order, raw []byte) (sdk.Order, error) {
	provider, ok := s.Payments.(payments.Refunder)
	if !ok || o.Payment == nil {
		return o, problem("refund_unavailable", "Refund provider unavailable.", 503, false)
	}
	if s.Payments.Name() != "demo" {
		remote, err := s.inspectPayment(ctx, o)
		if err != nil {
			return o, err
		}
		if remote.Status == "REFUNDED" {
			return s.finishRefund(ctx, o, raw, sdk.Refund{CaptureID: remote.CaptureID, Status: "COMPLETED", Amount: o.Amount, Currency: o.Currency, Evidence: "verified_capture_refunded_status"})
		}
		if o.Refund != nil && o.Refund.ID != "" {
			r, err := provider.InspectRefund(ctx, o.Refund.ID)
			if err != nil {
				return o, err
			}
			if r.Status == "COMPLETED" {
				return s.finishRefund(ctx, o, raw, r)
			}
			if r.Status == "PENDING" {
				return o, s.operationFailure(ctx, o, raw, "refund_pending")
			}
		}
	}
	o.Operation.NeedsReview = true
	o.Operation.LastCode = "refund_requires_review"
	s.logger().WarnContext(ctx, "refund_requires_review", "order_id", o.ID, "operation_id", o.Operation.ID, "code", "refund_requires_review")
	s.event(&o, "refund_requires_review", "Uncertain refund needs merchant/provider review; background recovery never issues a refund")
	return o, s.Save(ctx, o, raw)
}
func (s *Service) SyncPayment(ctx context.Context, a Actor, id string) (sdk.Order, error) {
	if err := human(a); err != nil {
		return sdk.Order{}, err
	}
	o, raw, err := s.Load(ctx, id, a.Owner)
	if err != nil {
		return o, err
	}
	if o.Payment == nil || o.Payment.CaptureID == "" || o.ApprovedDigest != approvals.Digest(o.Invoice) {
		return o, problem("capture_required", "Capture is required before lifecycle checks.", 409, false)
	}
	if s.Payments.Name() == "demo" {
		return o, nil
	}
	remote, err := s.inspectPayment(ctx, o)
	if err != nil {
		return o, err
	}
	if remote.Status == "REFUNDED" {
		return s.finishRefund(ctx, o, raw, sdk.Refund{CaptureID: remote.CaptureID, Status: "COMPLETED", Amount: o.Amount, Currency: o.Currency, Evidence: "verified_capture_refunded_status"})
	}
	if remote.Status != "COMPLETED" && o.State != "payment_review" {
		o.State = "payment_review"
		s.event(&o, "payment_requires_review", "Capture status is "+remote.Status+"; resolve partial refunds or failures with PayPal")
		return o, s.Save(ctx, o, raw)
	}
	return o, nil
}
