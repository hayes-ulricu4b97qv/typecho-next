package config

import (
	"testing"
	"time"
)

func TestFromEnvLoadsPositiveRateLimitsAndDefaults(t *testing.T) {
	for _, key := range []string{"APP_LOGIN_FAILURE_LIMIT", "APP_COMMENT_RATE_LIMIT", "APP_PASSWORD_RESET_LIMIT", "APP_PASSWORD_RESET_ACCOUNT_LIMIT", "APP_SESSION_TTL_MIN", "APP_TRUSTED_PROXIES", "APP_WRITE_TIMEOUT", "APP_LISTEN_HOST", "APP_VERSION"} {
		t.Setenv(key, "")
	}
	defaults := FromEnv()
	if defaults.LoginFailureLimit != 5 || defaults.CommentLimit != 30 || defaults.PasswordResetLimit != 5 || defaults.PasswordResetAccountLimit != 1 || defaults.SessionTTLMin != 1440 || defaults.WriteTimeout != 5*time.Minute || defaults.ListenHost != "127.0.0.1" || defaults.Version != BuildVersion {
		t.Fatalf("default settings = %#v", defaults)
	}
	t.Setenv("APP_LOGIN_FAILURE_LIMIT", "7")
	t.Setenv("APP_COMMENT_RATE_LIMIT", "40")
	t.Setenv("APP_PASSWORD_RESET_LIMIT", "3")
	t.Setenv("APP_PASSWORD_RESET_ACCOUNT_LIMIT", "2")
	t.Setenv("APP_SESSION_TTL_MIN", "90")
	t.Setenv("APP_WRITE_TIMEOUT", "12m")
	t.Setenv("APP_LISTEN_HOST", "0.0.0.0")
	t.Setenv("APP_TRUSTED_PROXIES", "127.0.0.1/32, ::1/128, ")
	configured := FromEnv()
	if configured.LoginFailureLimit != 7 || configured.CommentLimit != 40 || configured.PasswordResetLimit != 3 || configured.PasswordResetAccountLimit != 2 || configured.SessionTTLMin != 90 || configured.WriteTimeout != 12*time.Minute || configured.ListenHost != "0.0.0.0" {
		t.Fatalf("configured settings = %#v", configured)
	}
	if len(configured.TrustedProxyCIDRs) != 2 || configured.TrustedProxyCIDRs[0] != "127.0.0.1/32" || configured.TrustedProxyCIDRs[1] != "::1/128" {
		t.Fatalf("trusted proxies = %#v", configured.TrustedProxyCIDRs)
	}
	t.Setenv("APP_PASSWORD_RESET_LIMIT", "0")
	if got := FromEnv().PasswordResetLimit; got != 5 {
		t.Fatalf("invalid reset limit fallback = %d", got)
	}
	t.Setenv("APP_PASSWORD_RESET_ACCOUNT_LIMIT", "0")
	if got := FromEnv().PasswordResetAccountLimit; got != 1 {
		t.Fatalf("invalid per-account reset limit fallback = %d", got)
	}
	t.Setenv("APP_WRITE_TIMEOUT", "0s")
	if got := FromEnv().WriteTimeout; got != 5*time.Minute {
		t.Fatalf("invalid write timeout fallback = %s", got)
	}
	t.Setenv("APP_SESSION_TTL_MIN", "9223372036854775807")
	if got := FromEnv().SessionTTLMin; got != 1440 {
		t.Fatalf("overflowing session ttl fallback = %d", got)
	}
}

func TestFromEnvAllowsAppVersionOverride(t *testing.T) {
	t.Setenv("APP_VERSION", "2.3.4-rc.1")
	if got := FromEnv().Version; got != "2.3.4-rc.1" {
		t.Fatalf("version override = %q", got)
	}
}

func TestCookieSecureEnvRequiresExactTrue(t *testing.T) {
	for _, value := range []string{"", "false", "TRUE", "1"} {
		t.Setenv("APP_COOKIE_SECURE", value)
		if got := FromEnv().CookieSecure; got {
			t.Errorf("APP_COOKIE_SECURE=%q enabled Secure cookies", value)
		}
	}
	t.Setenv("APP_COOKIE_SECURE", "true")
	if got := FromEnv().CookieSecure; !got {
		t.Fatal("APP_COOKIE_SECURE=true did not enable Secure cookies")
	}
}
