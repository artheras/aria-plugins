package paypalreport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func tx(id, when, value, fee, status string) map[string]any {
	info := map[string]any{"transaction_id": id, "transaction_event_code": "T0006", "transaction_initiation_date": when, "transaction_status": status,
		"transaction_amount": map[string]string{"currency_code": "USD", "value": value}}
	if fee != "" {
		info["fee_amount"] = map[string]string{"currency_code": "USD", "value": fee}
	}
	return map[string]any{"transaction_info": info, "payer_info": map[string]any{"email_address": "buyer@example.com", "payer_name": map[string]string{"alternate_full_name": "A Buyer"}}}
}

func newTestClient(server *httptest.Server) *Client {
	c := NewSandbox("id", "secret")
	c.base, c.HTTP, c.sleep = server.URL, server.Client(), func(time.Duration) {}
	return c
}

func serve(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
			return
		}
		if r.Method != "GET" {
			t.Errorf("a reporting client sent %s %s", r.Method, r.URL.Path)
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return newTestClient(server)
}

func TestTransactionsSplitLongRangesIntoLegalWindowsAndFollowPages(t *testing.T) {
	var windows [][2]string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		from, _ := time.Parse(time.RFC3339, q.Get("start_date"))
		to, _ := time.Parse(time.RFC3339, q.Get("end_date"))
		if to.Sub(from) > MaxWindow {
			t.Errorf("window %s..%s exceeds 31 days", from, to)
		}
		if q.Get("fields") != "all" || q.Get("page_size") != "100" {
			t.Error("unexpected query", q)
		}
		page, _ := strconv.Atoi(q.Get("page"))
		if page == 1 {
			windows = append(windows, [2]string{q.Get("start_date"), q.Get("end_date")})
		}
		// Every window has two pages; the boundary transaction repeats across windows.
		details := []any{tx("T-"+strconv.Itoa(len(windows))+"-"+strconv.Itoa(page), "2026-09-01T10:00:00+0000", "10.00", "-0.59", "S")}
		if page == 2 {
			details = append(details, tx("SHARED", "2026-09-01T10:00:00+0000", "5.00", "-0.45", "S"))
		}
		json.NewEncoder(w).Encode(map[string]any{"transaction_details": details, "total_pages": 2})
	})
	start := time.Date(2026, 7, 1, 0, 0, 0, 123456789, time.UTC) // fractional seconds must not be sent
	end := start.Add(70 * 24 * time.Hour)
	got, err := c.Transactions(context.Background(), start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 3 {
		t.Fatalf("70 days should need 3 windows, got %d: %v", len(windows), windows)
	}
	for _, w := range windows {
		for _, s := range w {
			if len(s) != len("2026-07-01T00:00:00Z") {
				t.Fatalf("timestamp %q has fractional seconds or a zone offset", s)
			}
		}
	}
	// 3 windows x 2 pages, one distinct id per page, plus SHARED once.
	if len(got) != 7 {
		t.Fatalf("expected 6 distinct page rows + 1 shared, got %d", len(got))
	}
	if got[0].PayerEmail != "buyer@example.com" || got[0].Amount.Value != "10.00" || got[0].Initiated.IsZero() {
		t.Fatal("fields not decoded", got[0])
	}
}

func TestTokenIsRefreshedOnceAndRateLimitsAreRetried(t *testing.T) {
	var tokens, calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			atomic.AddInt32(&tokens, 1)
			json.NewEncoder(w).Encode(map[string]any{"access_token": "tok" + strconv.Itoa(int(atomic.LoadInt32(&tokens))), "expires_in": 3600})
			return
		}
		switch atomic.AddInt32(&calls, 1) {
		case 1:
			w.WriteHeader(401) // token expired early
		case 2:
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
		default:
			if r.Header.Get("Authorization") != "Bearer tok2" {
				t.Error("request did not use the refreshed token:", r.Header.Get("Authorization"))
			}
			json.NewEncoder(w).Encode(map[string]any{"balances": []any{map[string]any{"currency": "USD", "primary": true,
				"total_balance": map[string]string{"currency_code": "USD", "value": "100.00"}, "available_balance": map[string]string{"currency_code": "USD", "value": "90.00"}}}})
		}
	}))
	defer server.Close()
	c := newTestClient(server)
	got, err := c.Balances(context.Background(), time.Now())
	if err != nil || len(got) != 1 || got[0].Total.Value != "100.00" || got[0].Available.Value != "90.00" || !got[0].Primary {
		t.Fatal(got, err)
	}
	if tokens != 2 {
		t.Fatalf("expected one refresh, got %d token requests", tokens)
	}
}

func TestPermanentErrorsAreNotRetriedAndPersistentBusyGivesUp(t *testing.T) {
	var calls int32
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(403)
	})
	if _, err := c.Balances(context.Background(), time.Now()); err == nil {
		t.Fatal("403 accepted")
	}
	if calls != 1 {
		t.Fatalf("a 403 was retried %d times", calls)
	}
	calls = 0
	c = serve(t, func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1); w.WriteHeader(503) })
	if _, err := c.Balances(context.Background(), time.Now()); err == nil || calls != int32(maxRetries+1) {
		t.Fatal("persistent 503 should stop after the retry budget", err, calls)
	}
}

func TestSummarizeIsExactAndCountsOnlySuccessfulPerCurrency(t *testing.T) {
	usd := func(v, fee, status string) Transaction {
		f := &Amount{Currency: "USD", Value: fee}
		return Transaction{Status: status, Amount: Amount{Currency: "USD", Value: v}, Fee: f}
	}
	got := Summarize([]Transaction{
		usd("0.10", "-0.01", "S"), usd("0.20", "-0.01", "S"), // 0.1 + 0.2 must be exactly 0.30
		usd("999.00", "-9.00", "P"), usd("50.00", "-1.00", "D"),
		{Status: "S", Amount: Amount{Currency: "JPY", Value: "1500"}, Fee: &Amount{Currency: "JPY", Value: "-50"}},
	})
	if len(got) != 2 || got[0].Currency != "JPY" || got[1].Currency != "USD" {
		t.Fatal(got)
	}
	if u := got[1]; u.Count != 2 || u.Gross != "0.30" || u.Fees != "-0.02" || u.Net != "0.28" {
		t.Fatal("USD totals wrong or inexact", u)
	}
	if j := got[0]; j.Gross != "1500" || j.Net != "1450" {
		t.Fatal("zero-decimal currency formatted wrongly", j)
	}
}

func TestTransactionsRejectsAnEmptyRange(t *testing.T) {
	c := NewSandbox("id", "secret")
	now := time.Now()
	if _, err := c.Transactions(context.Background(), now, now); err == nil {
		t.Fatal("empty range accepted")
	}
}
