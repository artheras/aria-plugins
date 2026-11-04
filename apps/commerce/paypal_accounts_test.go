package commerce

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/artheras/aria-plugins/packages/agents"
	"github.com/artheras/aria-plugins/packages/payments"
	"github.com/artheras/aria-plugins/packages/paypalaccounts"
	"github.com/artheras/aria-plugins/packages/store"
)

func TestDefaultConnectionPageAndExactReviewLink(t *testing.T) {
	storage, _ := store.NewFile(t.TempDir())
	s := New(Config{Mode: "demo", Store: storage, Drafter: agents.Demo{}, Payments: payments.Demo{}, SessionSecret: strings.Repeat("s", 32)})
	for _, path := range []string{"/", "/?order=" + strings.Repeat("a", 32), "/?order=bad", "/review.html"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		raw := w.Body.String()
		if path == "/" {
			if w.Code != 200 || !strings.Contains(raw, "Your permissions.") || strings.Contains(raw, "connection-form") || strings.Contains(raw, "id=\"approve\"") {
				t.Fatal("default page exposed legacy workflows")
			}
		}
		if strings.Contains(path, "order=aaa") && (w.Code != 200 || !strings.Contains(raw, "Approve this exact order") || strings.Contains(raw, "connection-form")) {
			t.Fatal("exact review link broken")
		}
		if path == "/?order=bad" && w.Code != 400 {
			t.Fatal(w.Code)
		}
		if path == "/review.html" && w.Code != 404 {
			t.Fatal(w.Code)
		}
	}
}
func TestPayPalStatusRequiresUserSessionAndDoesNotInferSharedProvider(t *testing.T) {
	p := newTestIdentityProvider(t)
	_, web, _ := accountServer(t, p)
	b := newBrowser(t, web)
	b.request("GET", "/api/account/paypal", nil, 401)
	b = signedInAccount(t, web, p)
	var status paypalaccounts.Status
	if json.Unmarshal(b.request("GET", "/api/account/paypal", nil, 200), &status) != nil || status.Connected || status.State != "not_connected" {
		t.Fatal(status)
	}
	response, e := web.Client().Get(web.URL + "/connections.js")
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if strings.Contains(string(raw), "/api/catalog") || strings.Contains(string(raw), "/api/orders") {
		t.Fatal("default UI fetched demonstration records")
	}
}
