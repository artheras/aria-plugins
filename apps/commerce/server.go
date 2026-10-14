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
	workflow "github.com/artheras/aria-plugins/packages/commerce"
	merchantidentity "github.com/artheras/aria-plugins/packages/identity"
	"github.com/artheras/aria-plugins/packages/limits"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/artheras/aria-plugins/packages/sdk"
)

//go:embed web/*
var web embed.FS
var Catalog = []sdk.Product{
	{SKU: "TSH-001", Name: "Cotton tee", UnitAmount: 2400, Currency: "USD"},
	{SKU: "HAT-002", Name: "Everyday cap", UnitAmount: 1800, Currency: "USD"},
	{SKU: "MUG-003", Name: "Ceramic mug", UnitAmount: 1200, Currency: "USD"},
}

type Server struct {
	config  Config
	mux     *http.ServeMux
	service *workflow.Service
	limiter limits.Limiter
	logger  *slog.Logger
}

func New(c Config) *Server {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	s := &Server{config: c, mux: http.NewServeMux(), logger: c.Logger, limiter: limits.Limiter{Store: c.Store}, service: NewService(c)}
	s.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]string{"status": "ok", "mode": c.Mode})
	})
	s.mux.HandleFunc("GET /api/ready", s.ready)
	s.mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"mode": c.Mode, "version": sdk.Version, "contract_version": sdk.ContractVersion, "drafter": c.Drafter.Name(), "payment_provider": c.Payments.Name(), "merchant_login_available": c.MerchantToken != "", "plugins": []string{"commerce"}, "modules": []string{"agents", "approvals", "payments", "products", "storage"}})
	})
	s.mux.HandleFunc("GET /api/session", s.session)
	s.mux.HandleFunc("POST /api/login", s.login)
	s.mux.HandleFunc("GET /api/catalog", s.getCatalog)
	s.mux.HandleFunc("GET /api/catalog/snapshot", s.catalogSnapshot)
	s.mux.HandleFunc("POST /api/catalog/update", s.updateCatalog)
	s.mux.HandleFunc("GET /api/orders", s.listOrders)
	s.mux.HandleFunc("POST /api/orders", s.draft)
	s.mux.HandleFunc("GET /api/orders/{id}", s.getOrder)
	s.mux.HandleFunc("GET /api/orders/{id}/receipt", s.receipt)
	s.mux.HandleFunc("POST /api/orders/{id}/approve", s.approve)
	s.mux.HandleFunc("POST /api/orders/{id}/revise", s.revise)
	s.mux.HandleFunc("POST /api/orders/{id}/cancel", s.cancelOrder)
	s.mux.HandleFunc("POST /api/orders/{id}/resume", s.resumeOrder)
	s.mux.HandleFunc("POST /api/orders/{id}/refund", s.refundOrder)
	s.mux.HandleFunc("POST /api/orders/{id}/sync", s.syncPayment)
	s.mux.HandleFunc("POST /api/orders/{id}/checkout", s.checkout)
	s.mux.HandleFunc("POST /api/orders/{id}/capture", s.capture)
	s.mux.HandleFunc("POST /api/paypal/webhook", s.webhook)
	assets, _ := fs.Sub(web, "web")
	s.mux.Handle("GET /", http.FileServer(http.FS(assets)))
	return s
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := randomID()
	w.Header().Set("X-Request-ID", requestID)
	wrapped := &responseLog{ResponseWriter: w, status: 200}
	w = wrapped
	r = r.WithContext(workflow.WithRequestID(r.Context(), requestID))
	defer func() {
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		s.logger.InfoContext(r.Context(), "http_request", "request_id", requestID, "route", route, "method", r.Method, "status", wrapped.status, "duration_ms", time.Since(start).Milliseconds())
	}()
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
	code := "request_failed"
	switch status {
	case 400:
		code = "invalid_request"
	case 401:
		code = "authentication_required"
	case 403:
		code = "permission_denied"
	case 404:
		code = "not_found"
	case 409:
		code = "state_conflict"
	case 429:
		code = "rate_limited"
	case 503:
		code = "service_unavailable"
	}
	write(w, status, map[string]any{"error": message, "code": code, "retryable": status == 429 || status >= 500, "request_id": w.Header().Get("X-Request-ID")})
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

func (s *Server) merchantOwner() string {
	if s.config.MerchantID != "" {
		return merchantidentity.Owner(s.config.MerchantID)
	}
	return merchantidentity.LegacyOwner(s.config.SessionSecret, s.config.MerchantToken)
}
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
	if s.config.LegacyMerchantOwner != "" && data[0] == s.config.LegacyMerchantOwner {
		data[0] = s.merchantOwner()
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
	write(w, 200, map[string]any{"authenticated": true, "csrf": id.csrf, "merchant": id.owner == s.merchantOwner()})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	if !s.quota(w, r, "login", peer, s.config.loginLimit(), time.Minute) {
		return
	}
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
	write(w, 200, map[string]any{"authenticated": true, "csrf": id.csrf, "merchant": true})
}
func public(order sdk.Order) sdk.Order { order.Owner = ""; order.WebhookIDs = nil; return order }

// Kept for compatibility with internal tests; business behavior is in the service.
func (s *Server) load(ctx context.Context, id, owner string) (sdk.Order, []byte, error) {
	return s.service.Load(ctx, id, owner)
}
func (s *Server) save(ctx context.Context, o sdk.Order, expected []byte) error {
	return s.service.Save(ctx, o, expected)
}
func respondError(w http.ResponseWriter, err error) {
	var e *workflow.Error
	if errors.As(err, &e) {
		write(w, e.Status, map[string]any{"error": e.Message, "code": e.Code, "retryable": e.Retryable, "request_id": w.Header().Get("X-Request-ID")})
		return
	}
	fail(w, 503, "Service temporarily unavailable. Retry with the same order.")
}
func actor(id identity) workflow.Actor { return workflow.Actor{Owner: id.owner, Human: !id.bearer} }
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
	if !s.quota(w, r, "draft-global-day", "all", s.config.dailyLimit(), 24*time.Hour) || !s.quota(w, r, "draft-global-minute", "all", s.config.globalLimit(), time.Minute) || !s.quota(w, r, "draft-owner-minute", id.owner, s.config.draftLimit(), time.Minute) || !s.quota(w, r, "draft-owner-day", id.owner, s.config.ownerDailyLimit(), 24*time.Hour) {
		return
	}
	order, err := s.service.Draft(r.Context(), actor(id), body.Prompt)
	if err != nil {
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
	order, err := s.service.Get(r.Context(), actor(id), r.PathValue("id"))
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
	if order.State != "paid" && order.State != "refunded" && order.State != "refund_pending" && order.State != "payment_review" {
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
	order, err := s.service.Approve(r.Context(), actor(id), r.PathValue("id"), body.Digest)
	if err != nil {
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
	if !s.quota(w, r, "payment-action", id.owner, 30, time.Minute) {
		return
	}
	order, err := s.service.Checkout(r.Context(), actor(id), r.PathValue("id"))
	if err != nil {
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
	if !s.quota(w, r, "payment-action", id.owner, 30, time.Minute) {
		return
	}
	order, err := s.service.Capture(r.Context(), actor(id), r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	write(w, 200, public(order))
}
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		fail(w, 400, "Invalid webhook body.")
		return
	}
	result, err := s.service.Webhook(r.Context(), r.Header, raw)
	if err != nil {
		respondError(w, err)
		return
	}
	write(w, 200, map[string]string{"status": result})
}
func (s *Server) quota(w http.ResponseWriter, r *http.Request, scope, subject string, n int, window time.Duration) bool {
	retry, err := s.limiter.Take(r.Context(), scope, subject, n, window)
	if errors.Is(err, limits.ErrLimited) {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		fail(w, 429, "Request limit reached. Please wait before trying again.")
		return false
	}
	if err != nil {
		fail(w, 503, "Quota storage unavailable. Please try again later.")
		return false
	}
	return true
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	_, err := s.limiter.Take(ctx, "readiness", "probe", 1_000_000, time.Minute)
	if err != nil {
		fail(w, 503, "Storage is not ready.")
		return
	}
	write(w, 200, map[string]string{"status": "ready"})
}

type responseLog struct {
	http.ResponseWriter
	status int
}

func (w *responseLog) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseLog) Unwrap() http.ResponseWriter { return w.ResponseWriter }
