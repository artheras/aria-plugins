package commerce

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/artheras/aria-plugins/packages/agents"
	"github.com/artheras/aria-plugins/packages/payments"
	"github.com/artheras/aria-plugins/packages/store"
)

func TestLimitsErrorContractAndLogsDoNotLeakSecrets(t *testing.T) {
	storage, _ := store.NewFile(t.TempDir())
	var logs bytes.Buffer
	c := Config{Mode: "demo", PublicURL: "https://merchant.test", SessionSecret: strings.Repeat("s", 32), MerchantToken: strings.Repeat("m", 32), MerchantLoginToken: strings.Repeat("h", 32), Store: storage, Drafter: agents.Demo{}, Payments: payments.Demo{}, LoginLimit: 1, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	s := New(c)
	for _, status := range []int{401, 429} {
		r := httptest.NewRequest("POST", "/api/login?secret=not-for-logs", strings.NewReader(`{"token":"private-input"}`))
		r.Header.Set("Origin", c.PublicURL)
		r.RemoteAddr = "192.0.2.1:1000"
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != status || w.Header().Get("X-Request-ID") == "" {
			t.Fatal("missing status/request ID", w.Code)
		}
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		if result["code"] == nil || result["request_id"] != w.Header().Get("X-Request-ID") {
			t.Fatal("missing error contract", result)
		}
		if status == 429 && w.Header().Get("Retry-After") == "" {
			t.Fatal("missing backoff")
		}
	}
	if strings.Contains(logs.String(), "private-input") || strings.Contains(logs.String(), "not-for-logs") || strings.Contains(logs.String(), c.MerchantToken) || strings.Contains(logs.String(), c.MerchantLoginToken) {
		t.Fatal("secret logged")
	}
	if !strings.Contains(logs.String(), "request_id") || !strings.Contains(logs.String(), "duration_ms") {
		t.Fatal("missing structured request log")
	}
}

type unavailableStore struct{}

func (unavailableStore) Get(context.Context, string) ([]byte, error) {
	return nil, context.DeadlineExceeded
}
func (unavailableStore) CAS(context.Context, string, []byte, []byte) error {
	return context.DeadlineExceeded
}
func TestReadinessDistinguishesLivenessAndUnavailableStorage(t *testing.T) {
	c := Config{Mode: "demo", Store: unavailableStore{}, Drafter: agents.Demo{}, Payments: payments.Demo{}}
	s := New(c)
	for path, status := range map[string]int{"/api/health": 200, "/api/ready": 503} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != status {
			t.Fatal(path, w.Code)
		}
	}
}
func TestCloudRunRejectsEphemeralStorageAndWorkerNeedsNoAISecrets(t *testing.T) {
	for _, key := range []string{"ARIA_MODE", "ARIA_STORE", "K_SERVICE", "CLOUD_RUN_JOB", "VERCEL", "PUBLIC_URL", "ARIA_SESSION_SECRET", "ARIA_MERCHANT_TOKEN", "ARIA_MERCHANT_LOGIN_TOKEN", "PAYPAL_CLIENT_ID", "PAYPAL_CLIENT_SECRET", "AI_GATEWAY_MODEL", "AI_GATEWAY_API_KEY", "VERCEL_OIDC_TOKEN", "UPSTASH_REDIS_REST_URL", "UPSTASH_REDIS_REST_TOKEN"} {
		t.Setenv(key, "")
	}
	t.Setenv("K_SERVICE", "commerce")
	t.Setenv("PUBLIC_URL", "https://merchant.test")
	t.Setenv("ARIA_STORE", "file")
	if _, err := FromEnvironment(); err == nil || !strings.Contains(err.Error(), "Cloud Run") {
		t.Fatal("Cloud Run accepted files", err)
	}
	t.Setenv("K_SERVICE", "")
	t.Setenv("ARIA_MODE", "sandbox")
	t.Setenv("ARIA_DATA_DIR", t.TempDir())
	t.Setenv("PAYPAL_CLIENT_ID", "test-id")
	t.Setenv("PAYPAL_CLIENT_SECRET", "test-secret")
	c, err := FromEnvironmentForWorker()
	if err != nil || c.SessionSecret != "" {
		t.Fatal("worker requested browser/model secrets", err)
	}
	t.Setenv("ARIA_DAILY_DRAFT_LIMIT", "0")
	if _, err = FromEnvironmentForWorker(); err == nil {
		t.Fatal("invalid quota accepted")
	}
}
