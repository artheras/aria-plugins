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

func TestContentSecurityPolicyOpensToPayPalOnlyWhenButtonsAreOn(t *testing.T) {
	off := (&Server{config: Config{}}).csp()
	if strings.Contains(off, "paypal") || !strings.Contains(off, "script-src 'self';") {
		t.Fatal("default policy must not allow PayPal:", off)
	}
	on := (&Server{config: Config{PayPalButtons: true}}).csp()
	for _, want := range []string{"script-src 'self' https://www.paypal.com", "frame-src https://www.sandbox.paypal.com", "connect-src 'self' https://www.sandbox.paypal.com"} {
		if !strings.Contains(on, want) {
			t.Fatalf("buttons policy missing %q: %s", want, on)
		}
	}
	if !strings.Contains(on, "frame-ancestors 'none'") || !strings.Contains(on, "form-action 'self'") {
		t.Fatal("enabling buttons weakened the rest of the policy:", on)
	}
}

func TestAriaAIUsesLocalLoginOnlyOnTheUsersOwnMachine(t *testing.T) {
	for _, name := range []string{"PUBLIC_URL", "ARIA_SESSION_SECRET", "ARIA_MERCHANT_TOKEN", "AI_GATEWAY_MODEL", "AI_GATEWAY_API_KEY", "VERCEL_OIDC_TOKEN", "K_SERVICE", "CLOUD_RUN_JOB", "VERCEL"} {
		t.Setenv(name, "")
	}
	t.Setenv("ARIA_DATA_DIR", t.TempDir())
	t.Setenv("ARIA_MODE", "sandbox")
	t.Setenv("PUBLIC_URL", "http://127.0.0.1:4180")
	t.Setenv("PAYPAL_CLIENT_ID", "id")
	t.Setenv("PAYPAL_CLIENT_SECRET", "secret")
	t.Setenv("ARIA_MERCHANT_ID", "store-1")
	t.Setenv("ARIA_MERCHANT_TOKEN", strings.Repeat("m", 32))
	t.Setenv("ARIA_SESSION_SECRET", strings.Repeat("s", 32))
	t.Setenv("ARIA_AI", "aria")

	c, err := FromEnvironment()
	if err != nil {
		t.Fatal("no AI key should be needed with ARIA_AI=aria:", err)
	}
	if !strings.Contains(c.Drafter.Name(), "google/gemini") {
		t.Fatal("default model not applied:", c.Drafter.Name())
	}

	t.Setenv("K_SERVICE", "commerce")
	if _, err = FromEnvironment(); err == nil || !strings.Contains(err.Error(), "hosted") {
		t.Fatal("a hosted service was allowed to use a local login", err)
	}
}

func TestLocalParserWithRealSandboxPaymentsNeedsNoAIKeyAndIsLocalOnly(t *testing.T) {
	for _, name := range []string{"AI_GATEWAY_MODEL", "AI_GATEWAY_API_KEY", "VERCEL_OIDC_TOKEN", "K_SERVICE", "CLOUD_RUN_JOB", "VERCEL"} {
		t.Setenv(name, "")
	}
	t.Setenv("ARIA_DATA_DIR", t.TempDir())
	t.Setenv("ARIA_MODE", "sandbox")
	t.Setenv("PUBLIC_URL", "http://127.0.0.1:4180")
	t.Setenv("PAYPAL_CLIENT_ID", "id")
	t.Setenv("PAYPAL_CLIENT_SECRET", "secret")
	t.Setenv("ARIA_MERCHANT_ID", "store-1")
	t.Setenv("ARIA_MERCHANT_TOKEN", strings.Repeat("m", 32))
	t.Setenv("ARIA_SESSION_SECRET", strings.Repeat("s", 32))
	t.Setenv("ARIA_AI", "demo")
	c, err := FromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.Drafter.Name(), "no AI") || c.Payments.Name() != "paypal-sandbox" {
		t.Fatal("expected the local parser with real Sandbox payments:", c.Drafter.Name(), c.Payments.Name())
	}
	t.Setenv("K_SERVICE", "commerce")
	if _, err = FromEnvironment(); err == nil {
		t.Fatal("a hosted service accepted the local parser")
	}
}
