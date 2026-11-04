package commerce

import (
	"errors"
	"github.com/artheras/aria-plugins/packages/paypalaccounts"
	"net/http"
)

func (s *Server) paypalStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	status, e := s.config.PayPalAccounts.Status(r.Context(), id.principal.UserID)
	if e != nil {
		fail(w, 503, "PayPal connection status unavailable.")
		return
	}
	write(w, 200, struct {
		paypalaccounts.Status
		CanDisconnect bool `json:"can_disconnect"`
	}{status, s.config.PayPalVault != nil && status.Connected})
}
func paypalReadError(e error) error {
	switch {
	case errors.Is(e, paypalaccounts.ErrNotConnected):
		return paypalaccounts.ErrNotConnected
	case errors.Is(e, paypalaccounts.ErrPermission):
		return paypalaccounts.ErrPermission
	case errors.Is(e, paypalaccounts.ErrPage):
		return paypalaccounts.ErrPage
	default:
		return paypalaccounts.ErrUnavailable
	}
}

func (s *Server) paypalDisconnect(w http.ResponseWriter, r *http.Request) {
	id, ok := s.authorize(w, r, true)
	if !ok {
		return
	}
	if id.principal.UserID == "" {
		fail(w, 403, "Sign in to your Aria account first.")
		return
	}
	var input struct{}
	if decode(r, &input) != nil {
		fail(w, 400, "This action accepts no account IDs or credentials.")
		return
	}
	if s.config.PayPalVault == nil {
		fail(w, 503, "PayPal connection storage is not configured.")
		return
	}
	if e := s.config.PayPalVault.Disconnect(r.Context(), id.principal.UserID); e != nil {
		fail(w, 503, "Could not disconnect PayPal. Retry before assuming access has stopped.")
		return
	}
	s.logger.InfoContext(r.Context(), "paypal_account_disconnected", "user_id", id.principal.UserID, "request_id", w.Header().Get("X-Request-ID"))
	write(w, 200, map[string]any{"connected": false, "state": "not_connected", "provider_consent_revoked": false})
}
