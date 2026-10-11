package commerce

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/artheras/aria-plugins/packages/agents"
	"github.com/artheras/aria-plugins/packages/payments"
	"github.com/artheras/aria-plugins/packages/store"
)

type Config struct {
	Mode, PublicURL, SessionSecret, MerchantToken, DataDir string
	Drafter                                                agents.Drafter
	Payments                                               payments.Provider
	Store                                                  store.Store
}

func FromEnvironment() (Config, error) {
	c := Config{Mode: os.Getenv("ARIA_MODE"), PublicURL: os.Getenv("PUBLIC_URL"), SessionSecret: os.Getenv("ARIA_SESSION_SECRET"), MerchantToken: os.Getenv("ARIA_MERCHANT_TOKEN"), DataDir: os.Getenv("ARIA_DATA_DIR")}
	if c.Mode == "" {
		c.Mode = "demo"
	}
	if c.DataDir == "" {
		c.DataDir = "data"
	}
	if c.Mode != "demo" && c.Mode != "sandbox" {
		return c, errors.New("ARIA_MODE must be demo or sandbox")
	}
	if c.PublicURL == "" {
		if c.Mode == "sandbox" || os.Getenv("VERCEL") != "" {
			return c, errors.New("PUBLIC_URL is required")
		}
		port := os.Getenv("PORT")
		if port == "" {
			port = "4180"
		}
		c.PublicURL = "http://127.0.0.1:" + port
	}
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return c, errors.New("PUBLIC_URL must be an HTTPS origin, or HTTP localhost for development")
	}
	redisURL, redisToken := os.Getenv("UPSTASH_REDIS_REST_URL"), os.Getenv("UPSTASH_REDIS_REST_TOKEN")
	if redisURL != "" || redisToken != "" {
		r, err := url.Parse(redisURL)
		if err != nil || r.Scheme != "https" || r.Host == "" || r.User != nil || redisToken == "" {
			return c, errors.New("configure both HTTPS UPSTASH_REDIS_REST_URL and UPSTASH_REDIS_REST_TOKEN")
		}
		c.Store = store.Redis{URL: redisURL, Token: redisToken}
	} else {
		if os.Getenv("VERCEL") != "" {
			return c, errors.New("Vercel requires Redis persistence; set UPSTASH_REDIS_REST_URL and UPSTASH_REDIS_REST_TOKEN")
		}
		c.Store, err = store.NewFile(c.DataDir)
		if err != nil {
			return c, errors.New("cannot initialize local data directory")
		}
	}
	if c.MerchantToken != "" && len(c.MerchantToken) < 32 {
		return c, errors.New("ARIA_MERCHANT_TOKEN must contain at least 32 characters")
	}
	if c.SessionSecret != "" && len(c.SessionSecret) < 32 {
		return c, errors.New("ARIA_SESSION_SECRET must contain at least 32 characters")
	}
	if c.Mode == "sandbox" {
		required := []string{"PAYPAL_CLIENT_ID", "PAYPAL_CLIENT_SECRET", "AI_GATEWAY_MODEL", "ARIA_MERCHANT_TOKEN", "ARIA_SESSION_SECRET"}
		for _, key := range required {
			if os.Getenv(key) == "" {
				return c, errors.New("sandbox configuration missing " + key)
			}
		}
		key := os.Getenv("AI_GATEWAY_API_KEY")
		if key == "" {
			key = os.Getenv("VERCEL_OIDC_TOKEN")
		}
		if key == "" {
			return c, errors.New("set AI_GATEWAY_API_KEY or VERCEL_OIDC_TOKEN for sandbox mode")
		}
		c.Drafter = agents.Gateway{BaseURL: "https://ai-gateway.vercel.sh/v1", Key: key, Model: os.Getenv("AI_GATEWAY_MODEL")}
		c.Payments = payments.NewPayPal(os.Getenv("PAYPAL_CLIENT_ID"), os.Getenv("PAYPAL_CLIENT_SECRET"), os.Getenv("PAYPAL_WEBHOOK_ID"))
	} else {
		c.Drafter = agents.Demo{}
		c.Payments = payments.Demo{}
	}
	if c.SessionSecret == "" {
		if os.Getenv("VERCEL") != "" || redisURL != "" {
			return c, errors.New("ARIA_SESSION_SECRET is required with Redis persistence")
		}
		path := filepath.Join(c.DataDir, "session.key")
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			b = make([]byte, 32)
			if _, err = rand.Read(b); err != nil {
				return c, err
			}
			b = []byte(hex.EncodeToString(b))
			if err = os.WriteFile(path, b, 0600); err != nil {
				return c, err
			}
		} else if err != nil {
			return c, err
		}
		if len(b) < 32 {
			return c, errors.New("local session key is invalid")
		}
		c.SessionSecret = string(b)
	}
	return c, nil
}
