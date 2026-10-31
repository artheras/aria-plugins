package commerce

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	workflow "github.com/artheras/aria-plugins/packages/commerce"
	"github.com/artheras/aria-plugins/packages/products"
	"github.com/artheras/aria-plugins/packages/sdk"
	"github.com/artheras/aria-plugins/packages/store"
)

func (s *Server) catalogue() products.Repository {
	return s.service.ProductSource.(products.Repository)
}
func (s *Server) getCatalog(w http.ResponseWriter, r *http.Request) {
	items, err := s.catalogue().Products(r.Context())
	if err != nil {
		fail(w, 503, "Catalogue unavailable.")
		return
	}
	write(w, 200, items)
}
func (s *Server) catalogSnapshot(w http.ResponseWriter, r *http.Request) {
	items, err := s.catalogue().Snapshot(r.Context())
	if err != nil {
		fail(w, 503, "Catalogue unavailable.")
		return
	}
	write(w, 200, items)
}
func (s *Server) updateCatalog(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	if !s.config.loginEnabled() || id.owner != s.merchantOwner() {
		fail(w, 403, "Catalogue updates require the connected merchant session.")
		return
	}
	if !s.quota(w, r, "catalogue-update", id.owner, 10, time.Minute) {
		return
	}
	var body struct {
		Digest   string        `json:"digest"`
		Products []sdk.Product `json:"products"`
	}
	if decode(r, &body) != nil || products.Validate(body.Products) != nil {
		fail(w, 400, "Invalid catalogue products.")
		return
	}
	next, err := s.catalogue().Update(r.Context(), body.Digest, body.Products)
	if errors.Is(err, products.ErrStale) || errors.Is(err, store.ErrConflict) {
		fail(w, 409, "Catalogue changed. Reload before saving.")
		return
	}
	if err != nil {
		fail(w, 503, "Catalogue storage unavailable.")
		return
	}
	s.logger.InfoContext(r.Context(), "catalogue_updated", "request_id", w.Header().Get("X-Request-ID"), "product_count", len(next.Products))
	write(w, 200, next)
}
func (s *Server) cursorCipher() cipher.AEAD {
	key := sha256.Sum256([]byte("aria-order-cursor:" + s.config.SessionSecret))
	block, _ := aes.NewCipher(key[:])
	aead, _ := cipher.NewGCM(block)
	return aead
}
func cursorContext(owner string, o workflow.ListOptions) []byte {
	data, _ := json.Marshal([]any{owner, o.State, o.Query, o.ReviewOnly})
	return data
}
func (s *Server) sealCursor(cursor string, owner string, o workflow.ListOptions) string {
	if cursor == "" {
		return ""
	}
	aead := s.cursorCipher()
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic("secure random unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(cursor), cursorContext(owner, o)))
}
func (s *Server) openCursor(encoded, owner string, o workflow.ListOptions) (string, error) {
	if encoded == "" {
		return "", nil
	}
	if len(encoded) > 64<<10 {
		return "", errors.New("cursor too large")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	aead := s.cursorCipher()
	if err != nil || len(raw) < aead.NonceSize() {
		return "", errors.New("invalid cursor")
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], cursorContext(owner, o))
	return string(plain), err
}
func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, false)
	if !ok {
		return
	}
	if !s.quota(w, r, "order-list", id.owner, 60, time.Minute) {
		return
	}
	query := r.URL.Query()
	options := workflow.ListOptions{State: query.Get("state"), Query: query.Get("q"), ReviewOnly: query.Get("review") == "1"}
	if value := query.Get("limit"); value != "" {
		var err error
		options.Limit, err = strconv.Atoi(value)
		if err != nil {
			fail(w, 400, "Invalid page limit.")
			return
		}
	}
	cursor, err := s.openCursor(query.Get("cursor"), id.owner, options)
	if err != nil {
		fail(w, 400, "Invalid cursor for this session and filter.")
		return
	}
	options.Cursor = cursor
	page, err := s.service.List(r.Context(), actor(id), options)
	if err != nil {
		respondError(w, err)
		return
	}
	for i := range page.Orders {
		page.Orders[i] = public(page.Orders[i])
	}
	page.NextCursor = s.sealCursor(page.NextCursor, id.owner, options)
	write(w, 200, page)
}
func (s *Server) revise(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	if !s.quota(w, r, "order-manage", id.owner, 30, time.Minute) {
		return
	}
	var body struct {
		Digest string          `json:"digest"`
		Items  []sdk.Selection `json:"items"`
	}
	if decode(r, &body) != nil {
		fail(w, 400, "Invalid revision.")
		return
	}
	o, err := s.service.Revise(r.Context(), actor(id), r.PathValue("id"), body.Digest, body.Items)
	if err != nil {
		respondError(w, err)
		return
	}
	write(w, 200, public(o))
}
func (s *Server) manageOrder(w http.ResponseWriter, r *http.Request, resume bool) {
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	var body struct {
		Digest string `json:"digest"`
	}
	if decode(r, &body) != nil {
		fail(w, 400, "Review the current invoice before this action.")
		return
	}
	if !s.quota(w, r, "order-manage", id.owner, 30, time.Minute) {
		return
	}
	var o sdk.Order
	var err error
	if resume {
		o, err = s.service.Resume(r.Context(), actor(id), r.PathValue("id"), body.Digest)
	} else {
		o, err = s.service.Cancel(r.Context(), actor(id), r.PathValue("id"), body.Digest)
	}
	if err != nil {
		respondError(w, err)
		return
	}
	write(w, 200, public(o))
}
func (s *Server) cancelOrder(w http.ResponseWriter, r *http.Request) { s.manageOrder(w, r, false) }
func (s *Server) resumeOrder(w http.ResponseWriter, r *http.Request) { s.manageOrder(w, r, true) }
func (s *Server) refundOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	var body struct {
		Digest string `json:"digest"`
	}
	if decode(r, &body) != nil {
		fail(w, 400, "Review the captured invoice before refunding.")
		return
	}
	if !s.quota(w, r, "payment-action", id.owner, 30, time.Minute) {
		return
	}
	o, err := s.service.Refund(r.Context(), actor(id), r.PathValue("id"), body.Digest)
	if err != nil {
		respondError(w, err)
		return
	}
	write(w, 200, public(o))
}
func (s *Server) syncPayment(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	if !s.quota(w, r, "payment-action", id.owner, 30, time.Minute) {
		return
	}
	o, err := s.service.SyncPayment(r.Context(), actor(id), r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	write(w, 200, public(o))
}
