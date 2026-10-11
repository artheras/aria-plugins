package approvals

import (
	"github.com/artheras/aria-plugins/packages/sdk"
	"testing"
	"time"
)

func TestApprovalBindsInvoiceAndExpires(t *testing.T) {
	order := sdk.Order{Invoice: sdk.Invoice{ID: "id", Amount: 2400, Currency: "USD"}, State: "draft"}
	now := time.Now().UTC()
	if Approve(&order, Digest(order.Invoice), now) != nil || Check(order, now) != nil {
		t.Fatal("valid approval rejected")
	}
	order.Amount = 1200
	if Check(order, now) == nil {
		t.Fatal("changed price retained approval")
	}
	order.Amount = 2400
	if Check(order, now.Add(31*time.Minute)) == nil {
		t.Fatal("expired approval accepted")
	}
	other := sdk.Order{Invoice: order.Invoice, State: "draft"}
	if Approve(&other, "wrong", now) == nil || other.State != "draft" {
		t.Fatal("incorrect reviewed snapshot accepted")
	}
}
