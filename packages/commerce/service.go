// Package commerce owns the workflow independently of HTTP and MCP transports.
package commerce

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/artheras/aria-plugins/packages/agents"
	"github.com/artheras/aria-plugins/packages/approvals"
	"github.com/artheras/aria-plugins/packages/payments"
	"github.com/artheras/aria-plugins/packages/sdk"
	"github.com/artheras/aria-plugins/packages/store"
)

// Actor is supplied by a trusted transport after authentication, never by JSON input.
type Actor struct {
	Owner string
	Human bool
}
type Error struct {
	Code, Message string
	Status        int
	Retryable     bool
}

func (e *Error) Error() string { return e.Message }
func problem(code, message string, status int, retry bool) error {
	return &Error{code, message, status, retry}
}
func StorageError(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return problem("order_not_found", "Order not found.", 404, false)
	}
	if errors.Is(err, store.ErrConflict) {
		return problem("record_conflict", "Record changed. Reload the order before retrying.", 409, true)
	}
	return problem("storage_unavailable", "Storage unavailable. Retry with the same order.", 503, true)
}

type Service struct {
	Store         store.Store
	Drafter       agents.Drafter
	Payments      payments.Provider
	Catalog       []sdk.Product
	ProductSource interface {
		Products(context.Context) ([]sdk.Product, error)
	}
	MerchantOwner, LegacyOwner string
	PublicURL                  string
	Logger                     *slog.Logger
	Now                        func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("secure random unavailable")
	}
	return hex.EncodeToString(b)
}
func (s *Service) event(o *sdk.Order, action, detail string) {
	o.Events = append(o.Events, sdk.Event{At: s.now().Format(time.RFC3339), Action: action, Detail: detail})
}
func (s *Service) Load(ctx context.Context, id, owner string) (sdk.Order, []byte, error) {
	raw, err := s.Store.Get(ctx, id)
	if err != nil {
		return sdk.Order{}, nil, StorageError(err)
	}
	var o sdk.Order
	if json.Unmarshal(raw, &o) != nil || o.ID != id {
		return o, nil, problem("invalid_record", "Stored order is invalid.", 503, false)
	}
	if owner != "" && o.Owner != owner && !(owner == s.MerchantOwner && s.LegacyOwner != "" && o.Owner == s.LegacyOwner) {
		return sdk.Order{}, nil, StorageError(store.ErrNotFound)
	}
	return o, raw, nil
}
func (s *Service) Save(ctx context.Context, o sdk.Order, expected []byte) error {
	if s.LegacyOwner != "" && o.Owner == s.LegacyOwner {
		o.Owner = s.MerchantOwner
	}
	raw, err := json.Marshal(o)
	if err != nil {
		return err
	}
	if err = s.Store.CAS(ctx, o.ID, expected, raw); err != nil {
		return StorageError(err)
	}
	return nil
}
func (s *Service) Get(ctx context.Context, a Actor, id string) (sdk.Order, error) {
	if a.Owner == "" {
		return sdk.Order{}, problem("session_required", "Authenticated session required.", 401, false)
	}
	o, raw, err := s.Load(ctx, id, a.Owner)
	if err == nil {
		o, err = s.expire(ctx, o, raw)
	}
	return o, err
}
func human(a Actor) error {
	if a.Owner == "" {
		return problem("session_required", "Authenticated session required.", 401, false)
	}
	if !a.Human {
		return problem("human_confirmation_required", "This action requires human confirmation in the browser.", 403, false)
	}
	return nil
}
func (s *Service) Draft(ctx context.Context, a Actor, prompt string) (sdk.Order, error) {
	if len(prompt) < 3 || len(prompt) > 2000 {
		return sdk.Order{}, problem("invalid_prompt", "Prompt must contain 3 to 2000 characters.", 400, false)
	}
	if a.Owner == "" {
		return sdk.Order{}, problem("session_required", "Authenticated session required.", 401, false)
	}
	items := s.Catalog
	if s.ProductSource != nil {
		var err error
		items, err = s.ProductSource.Products(ctx)
		if err != nil {
			return sdk.Order{}, problem("catalogue_unavailable", "Catalogue unavailable. Try again later.", 503, true)
		}
	}
	d, err := s.Drafter.Draft(ctx, prompt, items)
	if err != nil {
		s.logger().WarnContext(ctx, "draft_failed", "request_id", requestID(ctx), "code", "draft_failed")
		return sdk.Order{}, problem("draft_failed", "Could not draft this request. Check the products and try again.", 422, false)
	}
	i, err := sdk.BuildInvoice(newID(), d.Items, items)
	if err != nil {
		return sdk.Order{}, problem("invalid_selection", err.Error(), 422, false)
	}
	o := sdk.Order{Invoice: i, Owner: a.Owner, State: "draft", Summary: d.Summary, CreatedAt: s.now().Format(time.RFC3339), Digest: approvals.Digest(i)}
	o.ExpiresAt = s.now().Add(24 * time.Hour).Format(time.RFC3339)
	s.event(&o, "draft_created", s.Drafter.Name()+"; prices resolved from server catalogue")
	err = s.Save(ctx, o, nil)
	return o, err
}
func (s *Service) Approve(ctx context.Context, a Actor, id, digest string) (sdk.Order, error) {
	if err := human(a); err != nil {
		return sdk.Order{}, err
	}
	o, raw, err := s.Load(ctx, id, a.Owner)
	if err != nil {
		return o, err
	}
	before := o.State
	if err = s.checkActive(o); err != nil {
		return o, err
	}
	if err = approvals.Approve(&o, digest, s.now()); err != nil {
		return o, problem("approval_rejected", err.Error(), 409, false)
	}
	if before == "draft" && o.State == "approved" {
		s.event(&o, "human_approved", "User confirmed the exact invoice snapshot")
	}
	err = s.Save(ctx, o, raw)
	return o, err
}
func (s *Service) intent(ctx context.Context, o *sdk.Order, raw []byte, kind string) ([]byte, error) {
	if o.Operation != nil {
		return nil, problem("operation_pending", "A payment operation is pending. Refresh the order while it is reconciled.", 409, true)
	}
	o.Operation = &sdk.Operation{ID: payments.RequestID(o.ID, kind), Kind: kind, StartedAt: s.now().Format(time.RFC3339), NextAttemptAt: s.now().Add(2 * time.Minute).Format(time.RFC3339), Attempts: 1}
	if err := s.Save(ctx, *o, raw); err != nil {
		return nil, err
	}
	return json.Marshal(o)
}
func (s *Service) Checkout(ctx context.Context, a Actor, id string) (sdk.Order, error) {
	if err := human(a); err != nil {
		return sdk.Order{}, err
	}
	o, raw, err := s.Load(ctx, id, a.Owner)
	if err != nil {
		return o, err
	}
	if o.State == "awaiting_payment" || o.State == "paid" {
		return o, nil
	}
	if o.State != "approved" {
		return o, problem("approval_required", "Approve the draft before checkout.", 409, false)
	}
	if err = s.checkActive(o); err != nil {
		return o, err
	}
	if err = approvals.Check(o, s.now()); err != nil {
		return o, problem("approval_expired", err.Error(), 409, false)
	}
	raw, err = s.intent(ctx, &o, raw, "create")
	if err != nil {
		return o, err
	}
	return s.create(ctx, o, raw)
}
func (s *Service) create(ctx context.Context, o sdk.Order, raw []byte) (sdk.Order, error) {
	u := s.PublicURL + "/?order=" + url.QueryEscape(o.ID)
	p, err := s.Payments.Create(ctx, o.Invoice, u, u+"&cancelled=1")
	if err != nil {
		return o, s.operationFailure(ctx, o, raw, "provider_unavailable")
	}
	if p.OrderID == "" || p.Currency != o.Currency || p.Amount != o.Amount {
		return o, s.operationFailure(ctx, o, raw, "payment_mismatch")
	}
	o.Payment = &p
	o.State = "awaiting_payment"
	o.Operation = nil
	s.event(&o, "checkout_created", s.Payments.Name()+" order "+p.OrderID+"; idempotent create request")
	err = s.Save(ctx, o, raw)
	return o, err
}
func (s *Service) Capture(ctx context.Context, a Actor, id string) (sdk.Order, error) {
	if err := human(a); err != nil {
		return sdk.Order{}, err
	}
	o, raw, err := s.Load(ctx, id, a.Owner)
	if err != nil {
		return o, err
	}
	if o.State == "paid" {
		return o, nil
	}
	if o.State == "refunded" || o.State == "refund_pending" || o.State == "payment_review" {
		return o, problem("capture_closed", "This captured order is in refund or provider review.", 409, false)
	}
	if o.State != "awaiting_payment" || o.Payment == nil || o.ApprovedDigest != approvals.Digest(o.Invoice) {
		return o, problem("checkout_required", "Checkout and exact invoice approval required.", 409, false)
	}
	raw, err = s.intent(ctx, &o, raw, "capture")
	if err != nil {
		return o, err
	}
	p, err := s.Payments.Capture(ctx, o.Invoice, o.Payment.OrderID)
	if err != nil {
		return o, s.operationFailure(ctx, o, raw, "provider_unavailable")
	}
	return s.finish(ctx, o, raw, p, "payment_reconciled")
}
func (s *Service) finish(ctx context.Context, o sdk.Order, raw []byte, p sdk.Payment, action string) (sdk.Order, error) {
	if o.State == "refunded" || o.State == "refund_pending" || o.State == "payment_review" || (o.Operation != nil && o.Operation.Kind == "refund") {
		return o, problem("capture_closed", "A refund or provider review is already in progress.", 409, false)
	}
	if o.Payment == nil || o.Payment.OrderID != p.OrderID || o.ApprovedDigest != approvals.Digest(o.Invoice) || payments.Validate(o.Invoice, p) != nil {
		return o, problem("payment_mismatch", "Provider payment does not match the approved invoice.", 502, false)
	}
	if o.State != "paid" {
		o.Payment = &p
		o.State = "paid"
		o.Operation = nil
		s.event(&o, action, s.Payments.Name()+"; capture "+p.CaptureID+" matched currency and amount")
	}
	err := s.Save(ctx, o, raw)
	return o, err
}
func (s *Service) operationFailure(ctx context.Context, o sdk.Order, raw []byte, code string) error {
	if o.Operation != nil {
		o.Operation.LastCode = code
		o.Operation.NextAttemptAt = s.now().Add(backoff(o.Operation.Attempts)).Format(time.RFC3339)
		_ = s.Save(ctx, o, raw)
	}
	s.logger().WarnContext(ctx, "payment_operation_pending", "request_id", requestID(ctx), "order_id", o.ID, "operation_id", o.Operation.ID, "code", code)
	return problem(code, "Payment outcome is pending. Retry with the same order after reconciliation.", 502, true)
}
func backoff(attempt int) time.Duration {
	if attempt > 8 {
		attempt = 8
	}
	return time.Duration(1<<attempt) * time.Minute
}
func (s *Service) Webhook(ctx context.Context, headers http.Header, body []byte) (string, error) {
	hook, err := s.Payments.Verify(ctx, headers, body)
	if err != nil {
		return "", problem("invalid_webhook", "Webhook signature or payload is invalid.", 400, false)
	}
	if hook.OrderID == "" {
		return "ignored", nil
	}
	snap, err := s.Payments.Inspect(ctx, hook.OrderID)
	if err != nil {
		return "", problem("provider_unavailable", "Provider unavailable. Retry webhook delivery.", 503, true)
	}
	o, raw, err := s.Load(ctx, snap.InvoiceID, "")
	if err != nil {
		return "", err
	}
	for _, id := range o.WebhookIDs {
		if id == hook.ID {
			return "duplicate", nil
		}
	}
	if len(o.WebhookIDs) >= 100 {
		return "", problem("webhook_limit", "Webhook event limit reached.", 409, false)
	}
	if o.Payment == nil || o.Payment.OrderID != hook.OrderID || o.ApprovedDigest != approvals.Digest(o.Invoice) {
		return "", problem("webhook_mismatch", "Webhook does not match an approved checkout.", 409, false)
	}
	if snap.Payment.Status == "REFUNDED" && o.Payment.CaptureID == snap.Payment.CaptureID {
		_, err = s.finishRefund(ctx, o, raw, sdk.Refund{CaptureID: snap.Payment.CaptureID, Status: "COMPLETED", Amount: snap.Payment.Amount, Currency: snap.Payment.Currency, Evidence: "verified_capture_refunded_status"})
		return "refund_reconciled", err
	}
	if payments.Validate(o.Invoice, snap.Payment) != nil {
		return "", problem("payment_mismatch", "Webhook payment does not match invoice.", 502, false)
	}
	o.WebhookIDs = append(o.WebhookIDs, hook.ID)
	if o.State == "refunded" || o.State == "refund_pending" || o.State == "payment_review" || (o.Operation != nil && o.Operation.Kind == "refund") {
		return "capture_already_recorded", nil
	}
	_, err = s.finish(ctx, o, raw, snap.Payment, "webhook_verified")
	if err != nil {
		return "", problem("storage_unavailable", "Storage conflict or failure. Retry webhook delivery.", 503, true)
	}
	return "reconciled", nil
}

// Recover never approves an order or initiates a Sandbox capture. It retries a
// persisted human checkout intent and inspects already requested captures.
func (s *Service) Recover(ctx context.Context, cursor string, limit int) (string, int, int, error) {
	scanner, ok := s.Store.(store.Scanner)
	if !ok {
		return "", 0, 0, errors.New("store does not support recovery scanning")
	}
	keys, next, err := scanner.Scan(ctx, cursor, limit)
	if err != nil {
		return cursor, 0, 0, err
	}
	recovered, pending := 0, 0
	for _, key := range keys {
		if strings.HasPrefix(key, "meta_") {
			continue
		}
		if ctx.Err() != nil {
			return cursor, recovered, pending, ctx.Err()
		}
		o, raw, err := s.Load(ctx, key, "")
		if err != nil {
			return cursor, recovered, pending, err
		}
		if _, err = s.expire(ctx, o, raw); err != nil {
			return cursor, recovered, pending, err
		}
		if o.Operation == nil || (o.State == "paid" && o.Operation.Kind != "refund") || o.State == "refunded" || o.Operation.NeedsReview {
			continue
		}
		pending++
		due, err := time.Parse(time.RFC3339, o.Operation.NextAttemptAt)
		if err != nil {
			return cursor, recovered, pending, errors.New("invalid recovery timestamp")
		}
		if s.now().Before(due) {
			continue
		}
		o.Operation.Attempts++
		o.Operation.NextAttemptAt = s.now().Add(2 * time.Minute).Format(time.RFC3339)
		if err = s.Save(ctx, o, raw); err != nil {
			var problem *Error
			if errors.As(err, &problem) && problem.Code == "record_conflict" {
				continue
			}
			return cursor, recovered, pending, err
		}
		raw, _ = json.Marshal(o)
		if o.Operation.Kind == "refund" {
			_, err = s.recoverRefund(ctx, o, raw)
		} else if o.Operation.Kind == "create" {
			if approvals.Check(o, s.now()) != nil {
				o.Operation.NeedsReview = true
				o.Operation.LastCode = "approval_expired"
				s.logger().WarnContext(ctx, "recovery_requires_review", "order_id", o.ID, "operation_id", o.Operation.ID, "code", "approval_expired")
				err = s.Save(ctx, o, raw)
			} else {
				_, err = s.create(ctx, o, raw)
			}
		} else if o.Operation.Kind == "capture" && o.Payment != nil && o.ApprovedDigest == approvals.Digest(o.Invoice) {
			var p sdk.Payment
			if s.Payments.Name() == "demo" {
				p, err = s.Payments.Capture(ctx, o.Invoice, o.Payment.OrderID)
			} else {
				var snap payments.Snapshot
				snap, err = s.Payments.Inspect(ctx, o.Payment.OrderID)
				p = snap.Payment
				if err == nil && snap.InvoiceID != o.ID {
					err = errors.New("provider invoice mismatch")
				}
			}
			if err == nil && payments.Validate(o.Invoice, p) == nil {
				_, err = s.finish(ctx, o, raw, p, "payment_reconciled")
			} else {
				// An unpaid capture needs another explicit browser action; no worker charge.
				if err == nil && (p.Status == "CREATED" || p.Status == "APPROVED") {
					o.Operation = nil
					err = s.Save(ctx, o, raw)
				} else {
					err = s.operationFailure(ctx, o, raw, "provider_pending")
				}
			}
		} else {
			o.Operation.NeedsReview = true
			o.Operation.LastCode = "invalid_operation"
			err = s.Save(ctx, o, raw)
		}
		if err == nil {
			recovered++
		} else {
			s.logger().WarnContext(ctx, "recovery_pending", "order_id", o.ID, "code", "recovery_pending")
		}
	}
	return next, recovered, pending, nil
}

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}
func requestID(ctx context.Context) string { id, _ := ctx.Value(requestIDKey{}).(string); return id }
