package commerce

import (
	"strings"
	"testing"
)

func TestConfigurationNeverFallsBackFromSandbox(t *testing.T) {
	for _, name := range []string{"ARIA_MODE", "PUBLIC_URL", "ARIA_SESSION_SECRET", "ARIA_MERCHANT_TOKEN", "UPSTASH_REDIS_REST_URL", "UPSTASH_REDIS_REST_TOKEN", "VERCEL", "PAYPAL_CLIENT_ID", "PAYPAL_CLIENT_SECRET", "AI_GATEWAY_MODEL", "AI_GATEWAY_API_KEY", "VERCEL_OIDC_TOKEN"} {
		t.Setenv(name, "")
	}
	t.Setenv("ARIA_DATA_DIR", t.TempDir())
	c, err := FromEnvironment()
	if err != nil || c.Mode != "demo" || len(c.SessionSecret) < 32 {
		t.Fatal("credential-free demo failed", err)
	}
	firstSecret := c.SessionSecret
	c, err = FromEnvironment()
	if err != nil || c.SessionSecret != firstSecret {
		t.Fatal("demo session key changed on restart")
	}
	t.Setenv("ARIA_MODE", "sandbox")
	t.Setenv("PUBLIC_URL", "http://127.0.0.1:4180")
	if _, err = FromEnvironment(); err == nil || !strings.Contains(err.Error(), "PAYPAL_CLIENT_ID") {
		t.Fatal("sandbox silently fell back to simulation", err)
	}
	t.Setenv("ARIA_MODE", "demo")
	t.Setenv("VERCEL", "1")
	if _, err = FromEnvironment(); err == nil || !strings.Contains(err.Error(), "Redis") {
		t.Fatal("Vercel accepted ephemeral order persistence", err)
	}
	t.Setenv("VERCEL", "")
	t.Setenv("PUBLIC_URL", "http://untrusted.example")
	if _, err = FromEnvironment(); err == nil {
		t.Fatal("insecure public origin accepted")
	}
}
