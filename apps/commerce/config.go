package commerce

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	workflow "github.com/artheras/aria-plugins/packages/commerce"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/artheras/aria-plugins/packages/agents"
	"github.com/artheras/aria-plugins/packages/payments"
	"github.com/artheras/aria-plugins/packages/store"
)

type Config struct {
	Mode, PublicURL, SessionSecret, MerchantToken, DataDir                          string
	Drafter                                                                         agents.Drafter
	Payments                                                                        payments.Provider
	Store                                                                           store.Store
	Logger                                                                          *slog.Logger
	DraftLimit, GlobalDraftLimit, DailyDraftLimit, OwnerDailyDraftLimit, LoginLimit int
	StorageKind                                                                     string
}

func FromEnvironment() (Config, error) { return fromEnvironment(false) }

// A recovery job does not need model credentials or browser/merchant secrets.
func FromEnvironmentForWorker() (Config, error) { return fromEnvironment(true) }
func fromEnvironment(worker bool) (Config, error) {
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
		if c.Mode == "sandbox" || (os.Getenv("VERCEL") != "" || os.Getenv("K_SERVICE") != "" || os.Getenv("CLOUD_RUN_JOB") != "") {
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
	c.StorageKind = os.Getenv("ARIA_STORE")
	if c.StorageKind == "" {
		if os.Getenv("K_SERVICE") != "" || os.Getenv("CLOUD_RUN_JOB") != "" {
			c.StorageKind = "firestore"
		} else if redisURL != "" || redisToken != "" {
			c.StorageKind = "redis"
		} else {
			c.StorageKind = "file"
		}
	}
	switch c.StorageKind {
	case "firestore":
		collection := os.Getenv("ARIA_FIRESTORE_COLLECTION")
		if collection == "" {
			collection = "aria_commerce_records"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c.Store, err = store.NewFirestore(ctx, os.Getenv("GOOGLE_CLOUD_PROJECT"), os.Getenv("ARIA_FIRESTORE_DATABASE"), collection)
		if err != nil {
			return c, err
		}
	case "redis":
		r, e := url.Parse(redisURL)
		if e != nil || r.Scheme != "https" || r.Host == "" || r.User != nil || redisToken == "" {
			return c, errors.New("configure both HTTPS UPSTASH_REDIS_REST_URL and UPSTASH_REDIS_REST_TOKEN")
		}
		c.Store = store.Redis{URL: redisURL, Token: redisToken}
	case "file":
		if os.Getenv("VERCEL") != "" || os.Getenv("K_SERVICE") != "" || os.Getenv("CLOUD_RUN_JOB") != "" {
			return c, errors.New("Cloud Run requires Firestore or Redis persistence; Vercel requires Redis persistence")
		}
		c.Store, err = store.NewFile(c.DataDir)
		if err != nil {
			return c, errors.New("cannot initialize local data directory")
		}
	default:
		return c, errors.New("ARIA_STORE must be file, redis, or firestore")
	}
	for _, v := range []struct {
		name   string
		target *int
	}{
		{"ARIA_DRAFT_LIMIT", &c.DraftLimit}, {"ARIA_GLOBAL_DRAFT_LIMIT", &c.GlobalDraftLimit}, {"ARIA_DAILY_DRAFT_LIMIT", &c.DailyDraftLimit}, {"ARIA_OWNER_DAILY_DRAFT_LIMIT", &c.OwnerDailyDraftLimit}, {"ARIA_LOGIN_LIMIT", &c.LoginLimit},
	} {
		if raw := os.Getenv(v.name); raw != "" {
			value, e := strconv.Atoi(raw)
			if e != nil || value < 1 || value > 1_000_000 {
				return c, errors.New(v.name + " must be between 1 and 1000000")
			}
			*v.target = value
		}
	}
	if c.MerchantToken != "" && len(c.MerchantToken) < 32 {
		return c, errors.New("ARIA_MERCHANT_TOKEN must contain at least 32 characters")
	}
	if c.SessionSecret != "" && len(c.SessionSecret) < 32 {
		return c, errors.New("ARIA_SESSION_SECRET must contain at least 32 characters")
	}
	if c.Mode == "sandbox" {
		required := []string{"PAYPAL_CLIENT_ID", "PAYPAL_CLIENT_SECRET"}
		if !worker {
			required = append(required, "AI_GATEWAY_MODEL", "ARIA_MERCHANT_TOKEN", "ARIA_SESSION_SECRET")
		}
		for _, key := range required {
			if os.Getenv(key) == "" {
				return c, errors.New("sandbox configuration missing " + key)
			}
		}
		key := os.Getenv("AI_GATEWAY_API_KEY")
		if key == "" {
			key = os.Getenv("VERCEL_OIDC_TOKEN")
		}
		if key == "" && !worker {
			return c, errors.New("set AI_GATEWAY_API_KEY or VERCEL_OIDC_TOKEN for sandbox mode")
		}
		c.Drafter = agents.Gateway{BaseURL: "https://ai-gateway.vercel.sh/v1", Key: key, Model: os.Getenv("AI_GATEWAY_MODEL")}
		c.Payments = payments.NewPayPal(os.Getenv("PAYPAL_CLIENT_ID"), os.Getenv("PAYPAL_CLIENT_SECRET"), os.Getenv("PAYPAL_WEBHOOK_ID"))
	} else {
		c.Drafter = agents.Demo{}
		c.Payments = payments.Demo{}
	}
	if c.SessionSecret == "" && !worker {
		if c.StorageKind != "file" {
			return c, errors.New("ARIA_SESSION_SECRET is required with persistent cloud storage")
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

func NewService(c Config) *workflow.Service {
	return &workflow.Service{Store: c.Store, Drafter: c.Drafter, Payments: c.Payments, Catalog: Catalog, PublicURL: c.PublicURL, Logger: c.Logger}
}
func positive(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
func (c Config) draftLimit() int      { return positive(c.DraftLimit, 10) }
func (c Config) globalLimit() int     { return positive(c.GlobalDraftLimit, 120) }
func (c Config) dailyLimit() int      { return positive(c.DailyDraftLimit, 500) }
func (c Config) ownerDailyLimit() int { return positive(c.OwnerDailyDraftLimit, 50) }
func (c Config) loginLimit() int      { return positive(c.LoginLimit, 10) }
