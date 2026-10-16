package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/artheras/aria-plugins/packages/sdk"
)

const SandboxURL = "https://api-m.sandbox.paypal.com"

type PayPal struct {
	ClientID, Secret, WebhookID string
	// Embedded creates orders for PayPal's JavaScript buttons, which collect the
	// buyer's approval in the page. The server still creates the order, captures
	// it and checks amount, currency and invoice; the browser only shows the
	// approval UI and is never trusted to say a payment happened.
	Embedded bool
	client   *http.Client
	base     string
	mu       sync.Mutex
	token    string
	expires  time.Time
}

func NewPayPal(clientID, secret, webhookID string) *PayPal {
	return &PayPal{ClientID: clientID, Secret: secret, WebhookID: webhookID, base: SandboxURL,
		client: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (*PayPal) Name() string { return "paypal-sandbox" }
func (p *PayPal) accessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.token != "" && time.Now().Before(p.expires) {
		return p.token, nil
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", p.base+"/v1/oauth2/token", strings.NewReader(url.Values{"grant_type": {"client_credentials"}}.Encode()))
	req.SetBasicAuth(p.ClientID, p.Secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := p.do(req, true)
	if err != nil {
		return "", errors.New("PayPal authentication unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", errors.New("PayPal rejected Sandbox credentials")
	}
	var out struct {
		Token   string `json:"access_token"`
		Expires int64  `json:"expires_in"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&out) != nil || out.Token == "" || out.Expires < 1 {
		return "", errors.New("invalid PayPal authentication response")
	}
	p.token = out.Token
	p.expires = time.Now().Add(time.Duration(out.Expires)*time.Second - 30*time.Second)
	return p.token, nil
}
func (p *PayPal) request(ctx context.Context, method, path, key string, body any, out any) error {
	token, err := p.accessToken(ctx)
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.base+path, reader)
	if err != nil {
		return errors.New("invalid PayPal request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "return=representation")
	if key != "" {
		req.Header.Set("PayPal-Request-Id", key)
	}
	// A call that carries PayPal-Request-Id, or only reads, can be repeated
	// without effect, so a dropped or stalled connection is retried.
	res, err := p.do(req, method == "GET" || key != "")
	if err != nil {
		return errors.New("PayPal unavailable; retry with the same order")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("PayPal request rejected (HTTP %d)", res.StatusCode)
	}
	if out != nil && json.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(out) != nil {
		return errors.New("invalid PayPal response")
	}
	return nil
}

// do sends a request, retrying transport failures (not HTTP responses) when it
// is safe to repeat. Connections through a proxy or a slow link sometimes stall
// in the TLS handshake; one retry usually succeeds, and without it the order is
// left in a "pending" state that needs a manual retry.
func (p *PayPal) do(req *http.Request, repeatable bool) (*http.Response, error) {
	var body []byte
	if req.GetBody != nil {
		if r, err := req.GetBody(); err == nil {
			body, _ = io.ReadAll(r)
			r.Close()
		}
	}
	attempts := 1
	if repeatable {
		attempts = 3
	}
	var res *http.Response
	var err error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(time.Duration(i) * 400 * time.Millisecond):
			}
			if req.GetBody != nil {
				req.Body, _ = req.GetBody()
			} else if body != nil {
				req.Body = io.NopCloser(bytes.NewReader(body))
			}
		}
		if res, err = p.client.Do(req); err == nil {
			return res, nil
		}
	}
	return nil, err
}

type money struct {
	Currency string `json:"currency_code"`
	Value    string `json:"value"`
}
type paypalOrder struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Links  []struct {
		Href string `json:"href"`
		Rel  string `json:"rel"`
	} `json:"links"`
	Units []struct {
		CustomID string `json:"custom_id"`
		Amount   money  `json:"amount"`
		Payments struct {
			Captures []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Amount money  `json:"amount"`
			} `json:"captures"`
		} `json:"payments"`
	} `json:"purchase_units"`
}

var centsPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})\.[0-9]{2}$`)

func parseCents(value string) (int64, error) {
	if !centsPattern.MatchString(value) {
		return 0, errors.New("invalid provider amount")
	}
	parts := strings.Split(value, ".")
	whole, _ := strconv.ParseInt(parts[0], 10, 64)
	fraction, _ := strconv.ParseInt(parts[1], 10, 64)
	return whole*100 + fraction, nil
}
func snapshot(order paypalOrder) (Snapshot, error) {
	if order.ID == "" || len(order.Units) != 1 || order.Units[0].CustomID == "" {
		return Snapshot{}, errors.New("PayPal order is missing invoice identity")
	}
	unit := order.Units[0]
	amount, err := parseCents(unit.Amount.Value)
	if err != nil {
		return Snapshot{}, err
	}
	p := sdk.Payment{Provider: "paypal-sandbox", OrderID: order.ID, Status: order.Status, Amount: amount, Currency: unit.Amount.Currency}
	if order.Status == "COMPLETED" {
		if len(unit.Payments.Captures) != 1 {
			return Snapshot{}, errors.New("expected one verified capture")
		}
		c := unit.Payments.Captures[0]
		captured, err := parseCents(c.Amount.Value)
		if err != nil {
			return Snapshot{}, err
		}
		if (c.Status != "COMPLETED" && c.Status != "REFUNDED" && c.Status != "PARTIALLY_REFUNDED" && c.Status != "DECLINED" && c.Status != "FAILED") || c.ID == "" || c.Amount.Currency != unit.Amount.Currency || captured != amount {
			return Snapshot{}, errors.New("PayPal capture is pending or mismatched")
		}
		p.CaptureID = c.ID
		p.Status = c.Status
	}
	return Snapshot{InvoiceID: unit.CustomID, Payment: p}, nil
}
func (p *PayPal) Create(ctx context.Context, i sdk.Invoice, returnURL, cancelURL string) (sdk.Payment, error) {
	body := map[string]any{
		"intent":         "CAPTURE",
		"purchase_units": []map[string]any{{"custom_id": i.ID, "description": "Aria Commerce reviewed order", "amount": money{Currency: i.Currency, Value: sdk.Decimal(i.Amount)}}},
	}
	if !p.Embedded {
		body["payment_source"] = map[string]any{"paypal": map[string]any{"experience_context": map[string]string{"return_url": returnURL, "cancel_url": cancelURL, "user_action": "PAY_NOW", "shipping_preference": "NO_SHIPPING"}}}
	}
	var out paypalOrder
	if err := p.request(ctx, "POST", "/v2/checkout/orders", RequestID(i.ID, "create"), body, &out); err != nil {
		return sdk.Payment{}, err
	}
	s, err := snapshot(out)
	if err != nil {
		return sdk.Payment{}, err
	}
	if s.InvoiceID != i.ID || s.Payment.Amount != i.Amount || s.Payment.Currency != i.Currency {
		return sdk.Payment{}, errors.New("PayPal order does not match approved invoice")
	}
	if p.Embedded {
		// No redirect link: the buttons approve this order ID in the page.
		return s.Payment, nil
	}
	for _, link := range out.Links {
		if link.Rel == "payer-action" || link.Rel == "approve" {
			u, err := url.Parse(link.Href)
			if err == nil && u.Scheme == "https" && u.Hostname() == "www.sandbox.paypal.com" && u.User == nil {
				s.Payment.CheckoutURL = link.Href
				break
			}
		}
	}
	if s.Payment.CheckoutURL == "" {
		return sdk.Payment{}, errors.New("PayPal did not return a Sandbox checkout link")
	}
	return s.Payment, nil
}
func (p *PayPal) Inspect(ctx context.Context, id string) (Snapshot, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`).MatchString(id) {
		return Snapshot{}, errors.New("invalid PayPal order ID")
	}
	var out paypalOrder
	if err := p.request(ctx, "GET", "/v2/checkout/orders/"+id, "", nil, &out); err != nil {
		return Snapshot{}, err
	}
	if out.ID != id {
		return Snapshot{}, errors.New("PayPal returned a different order")
	}
	return snapshot(out)
}
func (p *PayPal) Capture(ctx context.Context, i sdk.Invoice, id string) (sdk.Payment, error) {
	before, err := p.Inspect(ctx, id)
	if err != nil {
		return sdk.Payment{}, err
	}
	if before.InvoiceID != i.ID || before.Payment.Amount != i.Amount || before.Payment.Currency != i.Currency {
		return sdk.Payment{}, errors.New("PayPal order does not match approved invoice")
	}
	if before.Payment.Status == "COMPLETED" {
		return before.Payment, Validate(i, before.Payment)
	}
	if before.Payment.Status != "APPROVED" {
		return sdk.Payment{}, errors.New("complete approval in PayPal Sandbox before capture")
	}
	var out paypalOrder
	captureErr := p.request(ctx, "POST", "/v2/checkout/orders/"+id+"/capture", RequestID(i.ID, "capture"), map[string]any{}, &out)
	// Always reconcile from the provider after capture, including ambiguous retries.
	after, err := p.Inspect(ctx, id)
	if err != nil {
		return sdk.Payment{}, err
	}
	if after.InvoiceID != i.ID {
		return sdk.Payment{}, errors.New("PayPal capture does not match invoice")
	}
	if err = Validate(i, after.Payment); err != nil {
		if captureErr != nil {
			return sdk.Payment{}, captureErr
		}
		return sdk.Payment{}, err
	}
	return after.Payment, nil
}
func (p *PayPal) Verify(ctx context.Context, headers http.Header, raw []byte) (Webhook, error) {
	if p.WebhookID == "" {
		return Webhook{}, errors.New("PAYPAL_WEBHOOK_ID is not configured")
	}
	names := map[string]string{"auth_algo": "Paypal-Auth-Algo", "cert_url": "Paypal-Cert-Url", "transmission_id": "Paypal-Transmission-Id", "transmission_sig": "Paypal-Transmission-Sig", "transmission_time": "Paypal-Transmission-Time"}
	body := map[string]any{"webhook_id": p.WebhookID, "webhook_event": json.RawMessage(raw)}
	for field, header := range names {
		value := headers.Get(header)
		if value == "" || len(value) > 4096 {
			return Webhook{}, errors.New("missing webhook verification headers")
		}
		body[field] = value
	}
	var verified struct {
		Status string `json:"verification_status"`
	}
	if err := p.request(ctx, "POST", "/v1/notifications/verify-webhook-signature", "", body, &verified); err != nil {
		return Webhook{}, err
	}
	if verified.Status != "SUCCESS" {
		return Webhook{}, errors.New("invalid PayPal webhook signature")
	}
	var event struct {
		ID       string `json:"id"`
		Type     string `json:"event_type"`
		Resource struct {
			Supplementary struct {
				Related struct {
					OrderID string `json:"order_id"`
				} `json:"related_ids"`
			} `json:"supplementary_data"`
		} `json:"resource"`
	}
	if json.Unmarshal(raw, &event) != nil || event.ID == "" {
		return Webhook{}, errors.New("invalid webhook event")
	}
	if event.Type != "PAYMENT.CAPTURE.COMPLETED" {
		return Webhook{ID: event.ID}, nil
	}
	if event.Resource.Supplementary.Related.OrderID == "" {
		return Webhook{}, errors.New("webhook is missing related order ID")
	}
	return Webhook{ID: event.ID, OrderID: event.Resource.Supplementary.Related.OrderID}, nil
}
