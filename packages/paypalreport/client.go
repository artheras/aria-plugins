// Package paypalreport reads a PayPal account's transactions and balances.
//
// It is read-only by construction: the client can only issue GET requests to
// the reporting endpoints listed here, and nothing in this package creates an
// order, captures a payment or refunds one. It is the part of the plugin that
// can safely be pointed at a business's real account.
//
// Behaviour follows PayPal's Transaction Search documentation: a request may
// span at most 31 days, timestamps carry no fractional seconds, results are
// paged with page/total_pages, and new activity can take up to three hours to
// appear. The response shapes below are written from that documentation and
// have not yet been exercised against a live Sandbox account.
package paypalreport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	SandboxURL = "https://api-m.sandbox.paypal.com"
	LiveURL    = "https://api-m.paypal.com"

	// MaxWindow is the longest span one Transaction Search request may cover.
	MaxWindow = 31 * 24 * time.Hour

	maxBody    = 4 << 20
	pageSize   = 100
	maxRetries = 3
)

// Client reads from one PayPal account. Credentials stay in memory.
type Client struct {
	ClientID, Secret string
	HTTP             *http.Client

	base    string
	sleep   func(time.Duration)
	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewSandbox reads from PayPal's Sandbox.
func NewSandbox(clientID, secret string) *Client { return newClient(clientID, secret, SandboxURL) }

// NewLive reads from a real PayPal account. It is a separate constructor so
// that using real data is always a deliberate, visible choice in the caller.
func NewLive(clientID, secret string) *Client { return newClient(clientID, secret, LiveURL) }

func newClient(clientID, secret, base string) *Client {
	return &Client{ClientID: clientID, Secret: secret, base: base, sleep: time.Sleep,
		HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *Client) accessToken(ctx context.Context, force bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !force && c.token != "" && time.Now().Before(c.expires) {
		return c.token, nil
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/v1/oauth2/token", strings.NewReader(url.Values{"grant_type": {"client_credentials"}}.Encode()))
	if err != nil {
		return "", errors.New("invalid PayPal authentication request")
	}
	req.SetBasicAuth(c.ClientID, c.Secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", errors.New("PayPal authentication unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", errors.New("PayPal rejected these credentials")
	}
	var out struct {
		Token   string `json:"access_token"`
		Expires int64  `json:"expires_in"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&out) != nil || out.Token == "" || out.Expires < 1 {
		return "", errors.New("invalid PayPal authentication response")
	}
	c.token = out.Token
	c.expires = time.Now().Add(time.Duration(out.Expires)*time.Second - 30*time.Second)
	return c.token, nil
}

// get issues one GET with the retry policy PayPal's reporting API needs: a
// stale token is refreshed once, and 429 and 5xx responses are retried with
// the server's Retry-After or an exponential delay.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	refreshed := false
	var last error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		token, err := c.accessToken(ctx, false)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, "GET", c.base+path+"?"+query.Encode(), nil)
		if err != nil {
			return errors.New("invalid PayPal request")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		res, err := c.HTTP.Do(req)
		if err != nil {
			last = errors.New("PayPal unavailable")
			c.pause(ctx, attempt, "")
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
		res.Body.Close()
		switch {
		case res.StatusCode == 401 && !refreshed:
			refreshed = true
			if _, err = c.accessToken(ctx, true); err != nil {
				return err
			}
			attempt--
			continue
		case res.StatusCode == 429 || res.StatusCode >= 500:
			last = fmt.Errorf("PayPal busy (HTTP %d)", res.StatusCode)
			c.pause(ctx, attempt, res.Header.Get("Retry-After"))
			continue
		case res.StatusCode < 200 || res.StatusCode >= 300:
			return fmt.Errorf("PayPal request rejected (HTTP %d%s)", res.StatusCode, reason(body))
		}
		if readErr != nil || len(body) > maxBody {
			return errors.New("PayPal response unreadable or too large")
		}
		if json.Unmarshal(body, out) != nil {
			return errors.New("invalid PayPal response")
		}
		return nil
	}
	if last == nil {
		last = errors.New("PayPal request failed")
	}
	return last
}

func (c *Client) pause(ctx context.Context, attempt int, retryAfter string) {
	d := time.Duration(1<<attempt) * 500 * time.Millisecond
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds >= 0 && seconds <= 30 {
		d = time.Duration(seconds) * time.Second
	}
	select {
	case <-ctx.Done():
	default:
		c.sleep(d)
	}
}

// reason extracts PayPal's own error name and message, which say why a request
// was refused (for example that Transaction search is not enabled for the app).
// Both are PayPal's text, never our credentials.
func reason(body []byte) string {
	var e struct {
		Name    string `json:"name"`
		Message string `json:"message"`
		Error   string `json:"error"`
		Desc    string `json:"error_description"`
	}
	if json.Unmarshal(body, &e) != nil {
		return ""
	}
	parts := []string{}
	for _, v := range []string{e.Name, e.Error, e.Message, e.Desc} {
		if v = strings.TrimSpace(v); v != "" {
			if len(v) > 160 {
				v = v[:160]
			}
			parts = append(parts, v)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return ": " + strings.Join(parts, " - ")
}
