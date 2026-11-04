// Package paypalaccounts reads PayPal records using a user's independently
// verified merchant grant. It never uses the checkout service's shared credentials.
package paypalaccounts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNotConnected = errors.New("PayPal account not connected; complete merchant authorization first")
	ErrPermission   = errors.New("PayPal invoice read permission was not granted")
	ErrUnavailable  = errors.New("PayPal account service unavailable")
	ErrPage         = errors.New("page must be 1 to 10000 and page_size 1 to 100")
)

// GrantSource must resolve a server-verified provider grant for this exact Aria
// user, with a current access token from secure storage/refresh. No model or HTTP
// caller may supply these fields. Login with PayPal identity alone is insufficient
// for the invoices.read capability; the onboarding adapter must verify API access.
type GrantSource interface {
	Resolve(context.Context, string) (Grant, error)
}
type Grant struct {
	UserID, MerchantID, Environment string
	AccessToken                     string `json:"-"`
	Capabilities                    []string
	VerifiedAt, ExpiresAt           time.Time
}
type Status struct {
	Connected    bool       `json:"connected"`
	State        string     `json:"state"`
	MerchantID   string     `json:"merchant_id,omitempty"`
	Environment  string     `json:"environment,omitempty"`
	Capabilities []string   `json:"capabilities"`
	VerifiedAt   *time.Time `json:"verified_at,omitempty"`
}
type Invoice struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Amount struct {
		Currency string `json:"currency_code"`
		Value    string `json:"value"`
	} `json:"amount"`
}
type Page struct {
	Source      string    `json:"source"`
	MerchantID  string    `json:"merchant_id"`
	Environment string    `json:"environment"`
	RetrievedAt time.Time `json:"retrieved_at"`
	Page        int       `json:"page"`
	PageSize    int       `json:"page_size"`
	Items       []Invoice `json:"items"`
}
type Reader struct {
	Grants GrantSource
	client *http.Client
	base   string
}

func New(source GrantSource) *Reader {
	return &Reader{Grants: source, client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (r *Reader) grant(ctx context.Context, user string) (Grant, error) {
	if r == nil || r.Grants == nil || user == "" {
		return Grant{}, ErrNotConnected
	}
	g, e := r.Grants.Resolve(ctx, user)
	if e != nil {
		if errors.Is(e, ErrNotConnected) {
			return Grant{}, ErrNotConnected
		}
		return Grant{}, ErrUnavailable
	}
	if g.UserID != user || g.MerchantID == "" || g.AccessToken == "" || strings.ContainsAny(g.AccessToken, "\r\n") || g.VerifiedAt.IsZero() || g.VerifiedAt.After(time.Now()) || !g.ExpiresAt.After(time.Now().Add(15*time.Second)) || (g.Environment != "sandbox" && g.Environment != "production") {
		return Grant{}, ErrNotConnected
	}
	return g, nil
}
func (r *Reader) Status(ctx context.Context, user string) (Status, error) {
	g, e := r.grant(ctx, user)
	if errors.Is(e, ErrNotConnected) {
		return Status{State: "not_connected", Capabilities: []string{}}, nil
	}
	if e != nil {
		return Status{}, e
	}
	caps := []string{}
	if slices.Contains(g.Capabilities, "invoices.read") {
		caps = append(caps, "invoices.read")
	}
	return Status{Connected: true, State: "connected", MerchantID: g.MerchantID, Environment: g.Environment, Capabilities: caps, VerifiedAt: &g.VerifiedAt}, nil
}
func (r *Reader) Invoices(ctx context.Context, user string, page, size int) (Page, error) {
	if page < 1 || page > 10000 || size < 1 || size > 100 {
		return Page{}, ErrPage
	}
	g, e := r.grant(ctx, user)
	if e != nil {
		return Page{}, e
	}
	if !slices.Contains(g.Capabilities, "invoices.read") {
		return Page{}, ErrPermission
	}
	base := "https://api-m.sandbox.paypal.com"
	if g.Environment == "production" {
		base = "https://api-m.paypal.com"
	}
	if r.base != "" {
		base = r.base
	}
	query := url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(size)}, "total_required": {"false"}}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v2/invoicing/invoices?"+query.Encode(), nil)
	if e != nil {
		return Page{}, ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+g.AccessToken)
	req.Header.Set("Accept", "application/json")
	response, e := r.client.Do(req)
	if e != nil {
		return Page{}, ErrUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case 401:
		return Page{}, ErrNotConnected
	case 403:
		return Page{}, ErrPermission
	}
	if response.StatusCode != 200 {
		return Page{}, ErrUnavailable
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		return Page{}, ErrUnavailable
	}
	var result struct {
		Items []Invoice `json:"items"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Items) > size {
		return Page{}, ErrUnavailable
	}
	for _, item := range result.Items {
		if item.ID == "" || item.Status == "" {
			return Page{}, ErrUnavailable
		}
	}
	if result.Items == nil {
		result.Items = []Invoice{}
	}
	return Page{Source: "paypal_invoicing_v2", MerchantID: g.MerchantID, Environment: g.Environment, RetrievedAt: time.Now().UTC(), Page: page, PageSize: size, Items: result.Items}, nil
}
