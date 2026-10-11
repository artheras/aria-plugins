package sdk

import "testing"

func TestInvoiceUsesCatalogueAndIntegerCents(t *testing.T) {
	catalog := []Product{{SKU: "mug", Name: "Mug", UnitAmount: 1234, Currency: "USD"}}
	invoice, err := BuildInvoice("id", []Selection{{"mug", 2}, {"mug", 3}}, catalog)
	if err != nil || invoice.Amount != 6170 || len(invoice.Items) != 1 || invoice.Items[0].Quantity != 5 {
		t.Fatalf("unexpected invoice: %+v %v", invoice, err)
	}
	if Decimal(invoice.Amount) != "61.70" {
		t.Fatal("money formatting lost cents")
	}
	for _, items := range [][]Selection{{{"unknown", 1}}, {{"mug", 0}}, {{"mug", -1}}, {{"mug", 101}}, {{"mug", 60}, {"mug", 60}}, nil} {
		if _, err = BuildInvoice("id", items, catalog); err == nil {
			t.Fatalf("accepted invalid selection: %+v", items)
		}
	}
	if _, err = BuildInvoice("id", []Selection{{"mug", 100}}, []Product{{SKU: "mug", UnitAmount: 1 << 62, Currency: "USD"}}); err == nil {
		t.Fatal("accepted unsafe amount")
	}
}
