// Package sdk contains the provider-independent commerce contract.
package sdk

import (
	"errors"
	"fmt"
	"sort"
)

type Product struct {
	SKU        string `json:"sku"`
	Name       string `json:"name"`
	UnitAmount int64  `json:"unit_amount"`
	Currency   string `json:"currency"`
}
type Selection struct {
	SKU      string `json:"sku"`
	Quantity int64  `json:"quantity"`
}
type Line struct {
	Product
	Quantity int64 `json:"quantity"`
}
type Invoice struct {
	ID       string `json:"id"`
	Items    []Line `json:"items"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}
type Payment struct {
	Provider    string `json:"provider"`
	OrderID     string `json:"order_id"`
	CheckoutURL string `json:"checkout_url,omitempty"`
	CaptureID   string `json:"capture_id,omitempty"`
	Status      string `json:"status"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
}
type Event struct {
	At     string `json:"at"`
	Action string `json:"action"`
	Detail string `json:"detail"`
}
type Order struct {
	Invoice
	Owner          string   `json:"owner,omitempty"`
	State          string   `json:"state"`
	Summary        string   `json:"summary"`
	CreatedAt      string   `json:"created_at"`
	Digest         string   `json:"digest"`
	ApprovedDigest string   `json:"approved_digest,omitempty"`
	ApprovedAt     string   `json:"approved_at,omitempty"`
	Payment        *Payment `json:"payment,omitempty"`
	Events         []Event  `json:"events"`
	WebhookIDs     []string `json:"webhook_ids,omitempty"`
}

// BuildInvoice trusts only the server's catalogue for currency and prices.
func BuildInvoice(id string, selections []Selection, catalog []Product) (Invoice, error) {
	invoice := Invoice{ID: id, Currency: "USD"}
	if len(selections) == 0 || len(selections) > 20 {
		return invoice, errors.New("select between 1 and 20 catalogue items")
	}
	products := map[string]Product{}
	for _, p := range catalog {
		products[p.SKU] = p
	}
	counts := map[string]int64{}
	for _, s := range selections {
		p, ok := products[s.SKU]
		if !ok || p.Currency != "USD" || p.UnitAmount <= 0 {
			return invoice, errors.New("unknown or unavailable SKU")
		}
		if s.Quantity < 1 || s.Quantity > 100 || counts[s.SKU]+s.Quantity > 100 {
			return invoice, errors.New("quantity must be between 1 and 100 per SKU")
		}
		counts[s.SKU] += s.Quantity
	}
	keys := make([]string, 0, len(counts))
	for sku := range counts {
		keys = append(keys, sku)
	}
	sort.Strings(keys)
	for _, sku := range keys {
		p := products[sku]
		q := counts[sku]
		if p.UnitAmount > 1_000_000/q {
			return invoice, errors.New("order exceeds demo limit of USD 10,000")
		}
		invoice.Items = append(invoice.Items, Line{Product: p, Quantity: q})
		invoice.Amount += p.UnitAmount * q
		if invoice.Amount > 1_000_000 {
			return invoice, errors.New("order exceeds demo limit of USD 10,000")
		}
	}
	return invoice, nil
}

func Decimal(amount int64) string { return fmt.Sprintf("%d.%02d", amount/100, amount%100) }
