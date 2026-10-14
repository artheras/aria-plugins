package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/artheras/aria-plugins/packages/approvals"
	"github.com/artheras/aria-plugins/packages/sdk"
	"github.com/artheras/aria-plugins/packages/store"
)

var orderKey = regexp.MustCompile(`^[a-f0-9]{32}$`)

type ListOptions struct {
	Cursor, State, Query string
	Limit                int
	ReviewOnly           bool
}
type OrderPage struct {
	Orders     []sdk.Order `json:"orders"`
	NextCursor string      `json:"next_cursor,omitempty"`
}
type listPosition struct {
	Scan    string   `json:"scan"`
	Pending []string `json:"pending"`
	Done    bool     `json:"done"`
}

func (p listPosition) cursor() string {
	if p.Done && len(p.Pending) == 0 {
		return ""
	}
	raw, _ := json.Marshal(p)
	return string(raw)
}
func (s *Service) owns(owner, recordOwner string) bool {
	return owner == recordOwner || (owner == s.MerchantOwner && s.LegacyOwner != "" && recordOwner == s.LegacyOwner)
}

// List uses a bounded scan. The HTTP transport seals its storage cursor to the
// authenticated owner and filters. Paid records and other owners are never deleted.
func (s *Service) List(ctx context.Context, a Actor, options ListOptions) (OrderPage, error) {
	page := OrderPage{Orders: []sdk.Order{}}
	if a.Owner == "" {
		return page, problem("session_required", "Authenticated session required.", 401, false)
	}
	if options.Limit == 0 {
		options.Limit = 20
	}
	if options.Limit < 1 || options.Limit > 50 || len(options.Query) > 100 {
		return page, problem("invalid_filter", "Limit must be 1 to 50; search must be at most 100 characters.", 400, false)
	}
	if options.State != "" && !strings.Contains("|draft|approved|awaiting_payment|paid|cancelled|expired|refund_pending|refunded|payment_review|", "|"+options.State+"|") {
		return page, problem("invalid_filter", "Unknown order state.", 400, false)
	}
	scanner, ok := s.Store.(store.Scanner)
	if !ok {
		return page, problem("scan_unavailable", "Order listing is unavailable for this storage adapter.", 503, false)
	}
	position := listPosition{}
	if options.Cursor != "" && json.Unmarshal([]byte(options.Cursor), &position) != nil {
		return page, problem("invalid_cursor", "Invalid listing cursor.", 400, false)
	}
	query := strings.ToLower(strings.TrimSpace(options.Query))
	for scanned := 0; scanned < 500; {
		if len(position.Pending) == 0 {
			if position.Done {
				return page, nil
			}
			keys, next, err := scanner.Scan(ctx, position.Scan, 50)
			if err != nil {
				return page, StorageError(err)
			}
			if len(keys) > 100 {
				return page, problem("scan_limit", "Storage returned an oversized page.", 503, false)
			}
			position.Pending = keys
			position.Scan = next
			position.Done = next == ""
			if len(keys) == 0 {
				scanned++
				continue
			}
		}
		for len(position.Pending) > 0 && scanned < 500 {
			key := position.Pending[0]
			position.Pending = position.Pending[1:]
			scanned++
			if !orderKey.MatchString(key) {
				continue
			}
			o, raw, err := s.Load(ctx, key, "")
			if err != nil {
				return page, err
			}
			if !s.owns(a.Owner, o.Owner) {
				continue
			}
			o, err = s.expire(ctx, o, raw)
			if err != nil {
				return page, err
			}
			if options.State != "" && o.State != options.State {
				continue
			}
			if options.ReviewOnly && (o.Operation == nil || !o.Operation.NeedsReview) {
				continue
			}
			if query != "" {
				text := o.ID + " " + o.Summary
				for _, p := range o.Items {
					text += " " + p.SKU + " " + p.Name
				}
				if !strings.Contains(strings.ToLower(text), query) {
					continue
				}
			}
			page.Orders = append(page.Orders, o)
			if len(page.Orders) == options.Limit {
				page.NextCursor = position.cursor()
				return page, nil
			}
		}
	}
	page.NextCursor = position.cursor()
	return page, nil
}
func (s *Service) checkActive(o sdk.Order) error {
	if o.State == "cancelled" || o.State == "expired" {
		return problem("order_closed", "This order is closed. Create a new draft.", 409, false)
	}
	if o.ExpiresAt != "" && o.Payment == nil && o.Operation == nil && (o.State == "draft" || o.State == "approved") {
		expires, err := time.Parse(time.RFC3339, o.ExpiresAt)
		if err != nil {
			return problem("invalid_record", "Stored expiry is invalid.", 503, false)
		}
		if !s.now().Before(expires) {
			return problem("order_expired", "This unstarted order has expired. Create a new draft.", 409, false)
		}
	}
	return nil
}
func (s *Service) expire(ctx context.Context, o sdk.Order, raw []byte) (sdk.Order, error) {
	err := s.checkActive(o)
	var e *Error
	if errors.As(err, &e) && e.Code == "order_expired" {
		o.State = "expired"
		o.ApprovedDigest = ""
		o.ApprovedAt = ""
		s.event(&o, "order_expired", "Unstarted draft expired after 24 hours; no provider order was cancelled")
		return o, s.Save(ctx, o, raw)
	}
	if errors.As(err, &e) && e.Code == "invalid_record" {
		return o, err
	}
	return o, nil
}
func (s *Service) Revise(ctx context.Context, a Actor, id, digest string, items []sdk.Selection) (sdk.Order, error) {
	if err := human(a); err != nil {
		return sdk.Order{}, err
	}
	o, raw, err := s.Load(ctx, id, a.Owner)
	if err != nil {
		return o, err
	}
	if err = s.checkActive(o); err != nil {
		return o, err
	}
	if (o.State != "draft" && o.State != "approved") || o.Payment != nil || o.Operation != nil {
		return o, problem("invoice_locked", "Only an unstarted draft or approval may be revised.", 409, false)
	}
	if digest == "" || digest != o.Digest {
		return o, problem("invoice_changed", "Invoice changed. Reload before revising.", 409, true)
	}
	products := s.Catalog
	if s.ProductSource != nil {
		products, err = s.ProductSource.Products(ctx)
		if err != nil {
			return o, problem("catalogue_unavailable", "Catalogue unavailable.", 503, true)
		}
	}
	in, err := sdk.BuildInvoice(o.ID, items, products)
	if err != nil {
		return o, problem("invalid_selection", err.Error(), 422, false)
	}
	o.Invoice = in
	o.Digest = approvals.Digest(in)
	o.ApprovedDigest = ""
	o.ApprovedAt = ""
	o.State = "draft"
	s.event(&o, "draft_revised", "User revised selections using current server prices; a new exact-invoice approval is required")
	return o, s.Save(ctx, o, raw)
}
func (s *Service) Cancel(ctx context.Context, a Actor, id, digest string) (sdk.Order, error) {
	if err := human(a); err != nil {
		return sdk.Order{}, err
	}
	o, raw, err := s.Load(ctx, id, a.Owner)
	if err != nil {
		return o, err
	}
	if digest == "" || digest != o.Digest {
		return o, problem("invoice_changed", "Invoice changed. Reload before cancelling.", 409, true)
	}
	if o.State == "cancelled" {
		return o, nil
	}
	if err = s.checkActive(o); err != nil {
		return o, err
	}
	if (o.State != "draft" && o.State != "approved") || o.Payment != nil || o.Operation != nil {
		return o, problem("checkout_locked", "An existing or uncertain checkout cannot be cancelled locally. Resolve it with the provider.", 409, false)
	}
	o.State = "cancelled"
	o.ApprovedDigest = ""
	o.ApprovedAt = ""
	s.event(&o, "order_cancelled", "User cancelled an unstarted order; no payment was made")
	return o, s.Save(ctx, o, raw)
}

// Resume preserves the existing provider request ID. It never clears uncertain
// operations and never executes a capture; the bounded worker reconciles them.
func (s *Service) Resume(ctx context.Context, a Actor, id, digest string) (sdk.Order, error) {
	if err := human(a); err != nil {
		return sdk.Order{}, err
	}
	o, raw, err := s.Load(ctx, id, a.Owner)
	if err != nil {
		return o, err
	}
	if digest == "" || digest != o.Digest || digest != approvals.Digest(o.Invoice) {
		return o, problem("invoice_changed", "Review the current exact invoice before resuming.", 409, false)
	}
	if o.Operation == nil || !o.Operation.NeedsReview || (o.Operation.Kind != "create" && o.Operation.Kind != "capture") {
		return o, problem("review_unavailable", "This order has no supported operation awaiting review.", 409, false)
	}
	if o.Operation.Kind == "create" && o.Payment != nil {
		return o, problem("review_unavailable", "Checkout already exists.", 409, false)
	}
	if o.Operation.Kind == "capture" && (o.Payment == nil || o.ApprovedDigest != digest) {
		return o, problem("review_unavailable", "Existing checkout approval does not match.", 409, false)
	}
	o.ApprovedDigest = digest
	o.ApprovedAt = s.now().Format(time.RFC3339)
	o.Operation.NeedsReview = false
	o.Operation.NextAttemptAt = s.now().Format(time.RFC3339)
	s.event(&o, "human_resumed", "User reviewed the exact invoice and resumed the same durable operation; capture remains browser-only")
	return o, s.Save(ctx, o, raw)
}

// MigrateOwner is a bounded administration operation. Preview is the default;
// apply changes only the selected legacy ownership using compare-and-swap.
func MigrateOwner(ctx context.Context, storage store.Store, from, to, cursor string, limit int, apply bool) (string, int, error) {
	scanner, ok := storage.(store.Scanner)
	if !ok {
		return "", 0, errors.New("store cannot scan records")
	}
	keys, next, err := scanner.Scan(ctx, cursor, limit)
	if err != nil {
		return "", 0, err
	}
	count := 0
	for _, key := range keys {
		if !orderKey.MatchString(key) {
			continue
		}
		raw, err := storage.Get(ctx, key)
		if err != nil {
			return "", count, err
		}
		var o sdk.Order
		if json.Unmarshal(raw, &o) != nil || o.ID != key {
			return "", count, errors.New("invalid order during migration")
		}
		if o.Owner != from {
			continue
		}
		count++
		if apply {
			o.Owner = to
			data, _ := json.Marshal(o)
			if err = storage.CAS(ctx, key, raw, data); err != nil {
				return "", count, err
			}
		}
	}
	return next, count, nil
}
