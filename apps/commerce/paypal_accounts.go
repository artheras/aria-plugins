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
	write(w, 200, status)
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
