package payments

import (
	"context"
	"errors"
	"net/url"
	"regexp"

	"github.com/artheras/aria-plugins/packages/sdk"
)

// Refunder is an optional full-refund capability. InspectRefund never moves money.
type Refunder interface {
	Refund(context.Context, sdk.Invoice, string, string) (sdk.Refund, error)
	InspectRefund(context.Context, string) (sdk.Refund, error)
}

func ValidateRefund(i sdk.Invoice, captureID string, r sdk.Refund) error {
	if r.CaptureID != captureID || r.Amount != i.Amount || r.Currency != i.Currency || r.Status != "COMPLETED" || r.Evidence == "" {
		return errors.New("refund does not match the captured invoice")
	}
	return nil
}

type paypalRefund struct {
	ID     string                       `json:"id"`
	Status string                       `json:"status"`
	Amount money                        `json:"amount"`
	Links  []struct{ Rel, Href string } `json:"links"`
}

func (p *PayPal) refundSnapshot(out paypalRefund) (sdk.Refund, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`).MatchString(out.ID) {
		return sdk.Refund{}, errors.New("invalid PayPal refund identity")
	}
	amount, err := parseCents(out.Amount.Value)
	if err != nil {
		return sdk.Refund{}, err
	}
	result := sdk.Refund{ID: out.ID, Status: out.Status, Amount: amount, Currency: out.Amount.Currency, Evidence: "verified_refund_resource"}
	base, _ := url.Parse(p.base)
	for _, link := range out.Links {
		if link.Rel != "up" {
			continue
		}
		u, err := url.Parse(link.Href)
		if err != nil || u.Scheme != base.Scheme || u.Host != base.Host || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			continue
		}
		const prefix = "/v2/payments/captures/"
		if len(u.Path) > len(prefix) && u.Path[:len(prefix)] == prefix && regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`).MatchString(u.Path[len(prefix):]) {
			result.CaptureID = u.Path[len(prefix):]
		}
	}
	if result.CaptureID == "" {
		return sdk.Refund{}, errors.New("refund does not identify its captured payment")
	}
	return result, nil
}
func (p *PayPal) Refund(ctx context.Context, i sdk.Invoice, captureID, key string) (sdk.Refund, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`).MatchString(captureID) {
		return sdk.Refund{}, errors.New("invalid capture ID")
	}
	var out paypalRefund
	if err := p.request(ctx, "POST", "/v2/payments/captures/"+captureID+"/refund", key, map[string]any{"amount": money{Currency: i.Currency, Value: sdk.Decimal(i.Amount)}}, &out); err != nil {
		return sdk.Refund{}, err
	}
	r, err := p.refundSnapshot(out)
	if err != nil {
		return r, err
	}
	if r.CaptureID != captureID || r.Amount != i.Amount || r.Currency != i.Currency {
		return r, errors.New("refund response mismatch")
	}
	return r, nil
}
func (p *PayPal) InspectRefund(ctx context.Context, id string) (sdk.Refund, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`).MatchString(id) {
		return sdk.Refund{}, errors.New("invalid refund ID")
	}
	var out paypalRefund
	if err := p.request(ctx, "GET", "/v2/payments/refunds/"+id, "", nil, &out); err != nil {
		return sdk.Refund{}, err
	}
	if out.ID != id {
		return sdk.Refund{}, errors.New("refund ID mismatch")
	}
	return p.refundSnapshot(out)
}
func (Demo) Refund(_ context.Context, i sdk.Invoice, captureID, _ string) (sdk.Refund, error) {
	if captureID != "simulation_"+i.ID {
		return sdk.Refund{}, errors.New("demo refund capture mismatch")
	}
	return sdk.Refund{ID: "simrefund_" + i.ID, CaptureID: captureID, Status: "COMPLETED", Amount: i.Amount, Currency: i.Currency, Evidence: "simulated_refund"}, nil
}
func (Demo) InspectRefund(context.Context, string) (sdk.Refund, error) {
	return sdk.Refund{}, errors.New("demo refunds have no external record")
}
