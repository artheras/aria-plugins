// Package commerce exposes the merchant workflow as HTTP and embedded web UI.
package commerce

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/artheras/aria-plugins/packages/approvals"
	"github.com/artheras/aria-plugins/packages/payments"
	"github.com/artheras/aria-plugins/packages/sdk"
	"github.com/artheras/aria-plugins/packages/store"
)

//go:embed web/*
var web embed.FS
var Catalog = []sdk.Product{
	{SKU: "TSH-001", Name: "Cotton tee", UnitAmount: 2400, Currency: "USD"},
	{SKU: "HAT-002", Name: "Everyday cap", UnitAmount: 1800, Currency: "USD"},
	{SKU: "MUG-003", Name: "Ceramic mug", UnitAmount: 1200, Currency: "USD"},
}

type Server struct {
	config Config
	mux    *http.ServeMux
}

func New(c Config) *Server {
	s := &Server{config: c, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]string{"status": "ok", "mode": c.Mode})
	})
	s.mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"mode": c.Mode, "drafter": c.Drafter.Name(), "payment_provider": c.Payments.Name(), "merchant_login_available": c.MerchantToken != "", "plugins": []string{"agents", "approvals", "payments", "commerce"}})
	})
	s.mux.HandleFunc("GET /api/session", s.session)
	s.mux.HandleFunc("POST /api/login", s.login)
	s.mux.HandleFunc("GET /api/catalog", func(w http.ResponseWriter, r *http.Request) { write(w, 200, Catalog) })
	s.mux.HandleFunc("POST /api/orders", s.draft)
	s.mux.HandleFunc("GET /api/orders/{id}", s.getOrder)
	s.mux.HandleFunc("GET /api/orders/{id}/receipt", s.receipt)
	s.mux.HandleFunc("POST /api/orders/{id}/approve", s.approve)
	s.mux.HandleFunc("POST /api/orders/{id}/checkout", s.checkout)
	s.mux.HandleFunc("POST /api/orders/{id}/capture", s.capture)
	s.mux.HandleFunc("POST /api/paypal/webhook", s.webhook)
	assets, _ := fs.Sub(web, "web")
	s.mux.Handle("GET /", http.FileServer(http.FS(assets)))
	return s
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if r.Method == "POST" && r.URL.Path != "/api/paypal/webhook" {
		bearer := strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
		if (!bearer || r.URL.Path == "/api/login") && r.Header.Get("Origin") != s.config.PublicURL {
			fail(w, 403, "request origin does not match PUBLIC_URL")
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, message string) {
	write(w, status, map[string]string{"error": message})
}
func decode(r *http.Request, value any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return errors.New("invalid JSON request")
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return errors.New("unexpected trailing request content")
	}
	return nil
}
func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("secure random unavailable")
	}
	return hex.EncodeToString(b)
}
func (s *Server) sign(value string) string {
	h := hmac.New(sha256.New, []byte(s.config.SessionSecret))
	_, _ = h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}

type identity struct {
	owner, csrf string
	bearer      bool
}

func (s *Server) merchantOwner() string { return s.sign("merchant:" + s.config.MerchantToken) }
func (s *Server) setSession(w http.ResponseWriter, owner string) identity {
	value := base64.RawURLEncoding.EncodeToString([]byte(owner + ":" + strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10)))
	http.SetCookie(w, &http.Cookie{Name: "aria_session", Value: value + "." + s.sign(value), Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.config.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 86400})
	return identity{owner: owner, csrf: s.sign("csrf:" + value)}
}
func (s *Server) who(r *http.Request) (identity, error) {
	auth := r.Header.Get("Authorization")
	if auth != "" {
		expected := "Bearer " + s.config.MerchantToken
		if s.config.MerchantToken == "" || subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) != 1 {
			return identity{}, errors.New("invalid merchant token")
		}
		return identity{owner: s.merchantOwner(), bearer: true}, nil
	}
	c, err := r.Cookie("aria_session")
	if err != nil {
		return identity{}, errors.New("merchant session required")
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 || subtle.ConstantTimeCompare([]byte(s.sign(parts[0])), []byte(parts[1])) != 1 {
		return identity{}, errors.New("invalid session")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return identity{}, errors.New("invalid session")
	}
	data := strings.Split(string(raw), ":")
	if len(data) != 2 {
		return identity{}, errors.New("invalid session")
	}
	expires, err := strconv.ParseInt(data[1], 10, 64)
	if err != nil || time.Now().Unix() > expires {
		return identity{}, errors.New("session expired")
	}
	if s.config.Mode == "sandbox" && data[0] != s.merchantOwner() {
		return identity{}, errors.New("merchant session required")
	}
	return identity{owner: data[0], csrf: s.sign("csrf:" + parts[0])}, nil
}
func (s *Server) authorize(w http.ResponseWriter, r *http.Request, human bool) (identity, bool) {
	id, err := s.who(r)
	if err != nil {
		fail(w, 401, err.Error())
		return id, false
	}
	if human && id.bearer {
		fail(w, 403, "this action requires human confirmation in the browser")
		return id, false
	}
	if r.Method == "POST" && !id.bearer && subtle.ConstantTimeCompare([]byte(id.csrf), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
		fail(w, 403, "invalid session confirmation; reload the page")
		return id, false
	}
	return id, true
}
func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	id, err := s.who(r)
	if err != nil && s.config.Mode == "demo" {
		id = s.setSession(w, randomID())
		err = nil
	}
	if err != nil {
		write(w, 200, map[string]any{"authenticated": false})
		return
	}
	write(w, 200, map[string]any{"authenticated": true, "csrf": id.csrf})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if decode(r, &body) != nil {
		fail(w, 400, "invalid login request")
		return
	}
	if s.config.MerchantToken == "" || subtle.ConstantTimeCompare([]byte(body.Token), []byte(s.config.MerchantToken)) != 1 {
		fail(w, 401, "invalid merchant token")
		return
	}
	id := s.setSession(w, s.merchantOwner())
	write(w, 200, map[string]any{"authenticated": true, "csrf": id.csrf})
}
func event(order *sdk.Order, action, detail string) {
	order.Events = append(order.Events, sdk.Event{At: time.Now().UTC().Format(time.RFC3339), Action: action, Detail: detail})
}
func public(order sdk.Order) sdk.Order { order.Owner = ""; order.WebhookIDs = nil; return order }
func (s *Server) load(ctx context.Context, id, owner string) (sdk.Order, []byte, error) {
	raw, err := s.config.Store.Get(ctx, id)
	if err != nil {
		return sdk.Order{}, nil, err
	}
	var order sdk.Order
	if json.Unmarshal(raw, &order) != nil {
		return order, nil, errors.New("stored order is invalid")
	}
	if owner != "" && order.Owner != owner {
		return sdk.Order{}, nil, store.ErrNotFound
	}
	return order, raw, nil
}
func (s *Server) save(ctx context.Context, order sdk.Order, expected []byte) error {
	raw, err := json.Marshal(order)
	if err != nil {
		return err
	}
	return s.config.Store.CAS(ctx, order.ID, expected, raw)
}
func respondError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, 404, "order not found")
	case errors.Is(err, store.ErrConflict):
		fail(w, 409, err.Error())
	default:
		fail(w, 502, err.Error())
	}
}
func (s *Server) draft(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, false)
	if !ok {
		return
	}
	var body struct {
		Prompt string `json:"prompt"`
	}
	if decode(r, &body) != nil || len(body.Prompt) < 3 || len(body.Prompt) > 2000 {
		fail(w, 400, "prompt must contain 3 to 2000 characters")
		return
	}
	draft, err := s.config.Drafter.Draft(r.Context(), body.Prompt, Catalog)
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	invoice, err := sdk.BuildInvoice(randomID(), draft.Items, Catalog)
	if err != nil {
		fail(w, 422, err.Error())
		return
	}
	order := sdk.Order{Invoice: invoice, Owner: id.owner, State: "draft", Summary: draft.Summary, CreatedAt: time.Now().UTC().Format(time.RFC3339), Digest: approvals.Digest(invoice)}
	event(&order, "draft_created", s.config.Drafter.Name()+"; prices resolved from server catalogue")
	if err = s.save(r.Context(), order, nil); err != nil {
		respondError(w, err)
		return
	}
	write(w, 201, public(order))
}
func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, false)
	if !ok {
		return
	}
	order, _, err := s.load(r.Context(), r.PathValue("id"), id.owner)
	if err != nil {
		respondError(w, err)
		return
	}
	write(w, 200, public(order))
}
func (s *Server) receipt(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, false)
	if !ok {
		return
	}
	order, _, err := s.load(r.Context(), r.PathValue("id"), id.owner)
	if err != nil {
		respondError(w, err)
		return
	}
	if order.State != "paid" {
		fail(w, 409, "receipt is available after verified capture")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="aria-receipt.json"`)
	write(w, 200, map[string]any{"mode": s.config.Mode, "simulation": s.config.Mode == "demo", "order": public(order)})
}
func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	var body struct {
		Digest string `json:"digest"`
	}
	if decode(r, &body) != nil {
		fail(w, 400, "invalid approval request")
		return
	}
	order, raw, err := s.load(r.Context(), r.PathValue("id"), id.owner)
	if err != nil {
		respondError(w, err)
		return
	}
	if err = approvals.Approve(&order, body.Digest, time.Now()); err != nil {
		fail(w, 409, err.Error())
		return
	}
	if order.State == "approved" {
		if len(order.Events) == 1 {
			event(&order, "human_approved", "User confirmed the exact invoice snapshot")
		}
	}
	if err = s.save(r.Context(), order, raw); err != nil {
		respondError(w, err)
		return
	}
	write(w, 200, public(order))
}
func (s *Server) checkout(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	order, raw, err := s.load(r.Context(), r.PathValue("id"), id.owner)
	if err != nil {
		respondError(w, err)
		return
	}
	if order.State == "awaiting_payment" || order.State == "paid" {
		write(w, 200, public(order))
		return
	}
	if order.State != "approved" {
		fail(w, 409, "approve the draft before checkout")
		return
	}
	if err = approvals.Check(order, time.Now()); err != nil {
		fail(w, 409, err.Error())
		return
	}
	returnURL := s.config.PublicURL + "/?order=" + url.QueryEscape(order.ID)
	payment, err := s.config.Payments.Create(r.Context(), order.Invoice, returnURL, returnURL+"&cancelled=1")
	if err != nil {
		respondError(w, err)
		return
	}
	order.Payment = &payment
	order.State = "awaiting_payment"
	event(&order, "checkout_created", s.config.Payments.Name()+" order "+payment.OrderID+"; idempotent create request")
	if err = s.save(r.Context(), order, raw); err != nil {
		respondError(w, err)
		return
	}
	write(w, 200, public(order))
}
func (s *Server) capture(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	order, raw, err := s.load(r.Context(), r.PathValue("id"), id.owner)
	if err != nil {
		respondError(w, err)
		return
	}
	if order.State == "paid" {
		write(w, 200, public(order))
		return
	}
	if order.State != "awaiting_payment" || order.Payment == nil || order.ApprovedDigest != approvals.Digest(order.Invoice) {
		fail(w, 409, "checkout and exact invoice approval required")
		return
	}
	payment, err := s.config.Payments.Capture(r.Context(), order.Invoice, order.Payment.OrderID)
	if err != nil {
		respondError(w, err)
		return
	}
	if payment.OrderID != order.Payment.OrderID {
		fail(w, 502, "payment order identity mismatch")
		return
	}
	if err = payments.Validate(order.Invoice, payment); err != nil {
		respondError(w, err)
		return
	}
	order.Payment = &payment
	order.State = "paid"
	event(&order, "payment_reconciled", s.config.Payments.Name()+"; capture "+payment.CaptureID+" matched currency and amount")
	if err = s.save(r.Context(), order, raw); err != nil {
		respondError(w, err)
		return
	}
	write(w, 200, public(order))
}
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, 400, "invalid webhook body")
		return
	}
	hook, err := s.config.Payments.Verify(r.Context(), r.Header, raw)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if hook.OrderID == "" {
		write(w, 200, map[string]string{"status": "ignored"})
		return
	}
	snapshot, err := s.config.Payments.Inspect(r.Context(), hook.OrderID)
	if err != nil {
		respondError(w, err)
		return
	}
	order, expected, err := s.load(r.Context(), snapshot.InvoiceID, "")
	if err != nil {
		respondError(w, err)
		return
	}
	if order.Payment == nil || order.Payment.OrderID != hook.OrderID || order.ApprovedDigest != approvals.Digest(order.Invoice) {
		fail(w, 409, "webhook does not match an approved checkout")
		return
	}
	if err = payments.Validate(order.Invoice, snapshot.Payment); err != nil {
		respondError(w, err)
		return
	}
	for _, id := range order.WebhookIDs {
		if id == hook.ID {
			write(w, 200, map[string]string{"status": "duplicate"})
			return
		}
	}
	if len(order.WebhookIDs) >= 100 {
		fail(w, 409, "webhook event limit reached")
		return
	}
	order.WebhookIDs = append(order.WebhookIDs, hook.ID)
	order.Payment = &snapshot.Payment
	order.State = "paid"
	event(&order, "webhook_verified", "PayPal signature verified; provider capture reconciled")
	if err = s.save(r.Context(), order, expected); err != nil {
		fail(w, 503, "storage conflict or failure; retry webhook")
		return
	}
	write(w, 200, map[string]string{"status": "reconciled"})
}
