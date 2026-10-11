// Package payments separates commerce workflows from payment providers.
package payments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/artheras/aria-plugins/packages/sdk"
)

type Snapshot struct {
	InvoiceID string
	Payment   sdk.Payment
}
type Webhook struct{ ID, OrderID string }
type Provider interface {
	Name() string
	Create(context.Context, sdk.Invoice, string, string) (sdk.Payment, error)
	Capture(context.Context, sdk.Invoice, string) (sdk.Payment, error)
	Inspect(context.Context, string) (Snapshot, error)
	Verify(context.Context, http.Header, []byte) (Webhook, error)
}

func RequestID(id, operation string) string {
	h := sha256.Sum256([]byte(id + ":" + operation))
	return hex.EncodeToString(h[:])[:36]
}
func Validate(invoice sdk.Invoice, p sdk.Payment) error {
	if p.Status != "COMPLETED" || p.CaptureID == "" || p.Amount != invoice.Amount || p.Currency != invoice.Currency {
		return errors.New("payment is not completed or does not match the approved invoice")
	}
	return nil
}

type Demo struct{}

func (Demo) Name() string { return "demo" }
func (Demo) Create(_ context.Context, i sdk.Invoice, _, _ string) (sdk.Payment, error) {
	return sdk.Payment{Provider: "demo", OrderID: "demo_" + i.ID, Status: "CREATED", Amount: i.Amount, Currency: i.Currency}, nil
}
func (Demo) Capture(_ context.Context, i sdk.Invoice, id string) (sdk.Payment, error) {
	if id != "demo_"+i.ID {
		return sdk.Payment{}, errors.New("demo payment does not match invoice")
	}
	return sdk.Payment{Provider: "demo", OrderID: id, CaptureID: "simulation_" + i.ID, Status: "COMPLETED", Amount: i.Amount, Currency: i.Currency}, nil
}
func (Demo) Inspect(context.Context, string) (Snapshot, error) {
	return Snapshot{}, errors.New("demo payments have no external provider record")
}
func (Demo) Verify(context.Context, http.Header, []byte) (Webhook, error) {
	return Webhook{}, errors.New("webhooks require PayPal Sandbox mode")
}
