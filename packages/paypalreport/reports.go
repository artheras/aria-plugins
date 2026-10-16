package paypalreport

import (
	"context"
	"errors"
	"math/big"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// Amount is a PayPal money value: a decimal string and a currency. It is kept
// as a string so no precision is lost before it is summed exactly.
type Amount struct {
	Currency string `json:"currency_code"`
	Value    string `json:"value"`
}

// Transaction is the part of a Transaction Search result a reconciliation needs.
type Transaction struct {
	ID         string
	EventCode  string // PayPal T-code, e.g. T0006 for checkout payments
	Status     string // S success, P pending, D denied, V reversed, F partially refunded
	Initiated  time.Time
	Amount     Amount
	Fee        *Amount
	Subject    string
	InvoiceID  string
	PayerEmail string
	PayerName  string
}

type txPage struct {
	Details []struct {
		Info struct {
			ID        string  `json:"transaction_id"`
			Event     string  `json:"transaction_event_code"`
			Initiated string  `json:"transaction_initiation_date"`
			Amount    Amount  `json:"transaction_amount"`
			Fee       *Amount `json:"fee_amount"`
			Status    string  `json:"transaction_status"`
			Subject   string  `json:"transaction_subject"`
			InvoiceID string  `json:"invoice_id"`
		} `json:"transaction_info"`
		Payer struct {
			Email string `json:"email_address"`
			Name  struct {
				Alternate string `json:"alternate_full_name"`
			} `json:"payer_name"`
		} `json:"payer_info"`
	} `json:"transaction_details"`
	TotalPages int `json:"total_pages"`
}

// Transactions returns every transaction initiated in [start, end). Ranges
// longer than PayPal's 31-day limit are split into consecutive windows, and
// each window is read page by page. Results are ordered by initiation time and
// de-duplicated by transaction ID, since adjacent windows share a boundary.
func (c *Client) Transactions(ctx context.Context, start, end time.Time) ([]Transaction, error) {
	start, end = start.UTC().Truncate(time.Second), end.UTC().Truncate(time.Second)
	if !end.After(start) {
		return nil, errors.New("end must be after start")
	}
	seen := map[string]bool{}
	var all []Transaction
	for from := start; from.Before(end); {
		to := from.Add(MaxWindow)
		if to.After(end) {
			to = end
		}
		for page, total := 1, 1; page <= total; page++ {
			var out txPage
			q := url.Values{"start_date": {stamp(from)}, "end_date": {stamp(to)}, "fields": {"all"}, "page_size": {strconv.Itoa(pageSize)}, "page": {strconv.Itoa(page)}}
			if err := c.get(ctx, "/v1/reporting/transactions", q, &out); err != nil {
				return nil, err
			}
			if out.TotalPages > total {
				total = out.TotalPages
			}
			for _, d := range out.Details {
				if d.Info.ID == "" || seen[d.Info.ID] {
					continue
				}
				seen[d.Info.ID] = true
				when, _ := time.Parse("2006-01-02T15:04:05-0700", d.Info.Initiated)
				if when.IsZero() {
					when, _ = time.Parse(time.RFC3339, d.Info.Initiated)
				}
				all = append(all, Transaction{ID: d.Info.ID, EventCode: d.Info.Event, Status: d.Info.Status, Initiated: when.UTC(), Amount: d.Info.Amount, Fee: d.Info.Fee, Subject: d.Info.Subject, InvoiceID: d.Info.InvoiceID, PayerEmail: d.Payer.Email, PayerName: d.Payer.Name.Alternate})
			}
		}
		from = to
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Initiated.Before(all[j].Initiated) })
	return all, nil
}

// stamp formats a time the way Transaction Search requires: UTC, whole seconds.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z") }

// Balance is one currency's balance on the account.
type Balance struct {
	Currency  string
	Primary   bool
	Total     Amount
	Available Amount
	Withheld  *Amount
}

// Balances returns the account's balances as of the given time.
func (c *Client) Balances(ctx context.Context, asOf time.Time) ([]Balance, error) {
	var out struct {
		Balances []struct {
			Currency  string  `json:"currency"`
			Primary   bool    `json:"primary"`
			Total     Amount  `json:"total_balance"`
			Available Amount  `json:"available_balance"`
			Withheld  *Amount `json:"withheld_balance"`
		} `json:"balances"`
	}
	q := url.Values{"as_of_time": {stamp(asOf)}}
	if err := c.get(ctx, "/v1/reporting/balances", q, &out); err != nil {
		return nil, err
	}
	balances := make([]Balance, 0, len(out.Balances))
	for _, b := range out.Balances {
		balances = append(balances, Balance{Currency: b.Currency, Primary: b.Primary, Total: b.Total, Available: b.Available, Withheld: b.Withheld})
	}
	return balances, nil
}

// Totals is a currency's activity summed exactly.
type Totals struct {
	Currency string
	Count    int
	Gross    string // sum of transaction amounts (credits positive, debits negative)
	Fees     string // sum of fee amounts (PayPal reports fees as negative)
	Net      string // Gross + Fees
}

// Summarize sums successful (status S) transactions per currency with exact
// decimal arithmetic. Pending, denied and reversed items are counted by the
// caller if they matter; mixing them into a total would hide a mismatch.
func Summarize(transactions []Transaction) []Totals {
	type acc struct {
		count       int
		gross, fees *big.Rat
	}
	by := map[string]*acc{}
	for _, t := range transactions {
		if t.Status != "S" {
			continue
		}
		a := by[t.Amount.Currency]
		if a == nil {
			a = &acc{gross: new(big.Rat), fees: new(big.Rat)}
			by[t.Amount.Currency] = a
		}
		a.count++
		if v, ok := new(big.Rat).SetString(t.Amount.Value); ok {
			a.gross.Add(a.gross, v)
		}
		if t.Fee != nil && t.Fee.Currency == t.Amount.Currency {
			if v, ok := new(big.Rat).SetString(t.Fee.Value); ok {
				a.fees.Add(a.fees, v)
			}
		}
	}
	currencies := make([]string, 0, len(by))
	for c := range by {
		currencies = append(currencies, c)
	}
	sort.Strings(currencies)
	out := make([]Totals, 0, len(currencies))
	for _, c := range currencies {
		a := by[c]
		places := decimals(c)
		out = append(out, Totals{Currency: c, Count: a.count, Gross: a.gross.FloatString(places), Fees: a.fees.FloatString(places), Net: new(big.Rat).Add(a.gross, a.fees).FloatString(places)})
	}
	return out
}

// decimals is how many places a currency shows. PayPal has no minor unit for
// these three; everything else it supports uses two.
func decimals(currency string) int {
	switch currency {
	case "JPY", "HUF", "TWD":
		return 0
	}
	return 2
}
