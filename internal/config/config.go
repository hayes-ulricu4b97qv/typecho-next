package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// MaxSessionTTLMinutes is the largest whole-minute duration representable by time.Duration.
const MaxSessionTTLMinutes int64 = (1<<63 - 1) / int64(time.Minute)

// BuildVersion is replaced by release builds and can be overridden with APP_VERSION.
var BuildVersion = "0.1.0-dev"

// Settings contains runtime settings that are safe to expose to the HTTP layer.
type Settings struct {
	Version                   string
	ListenHost                string
	CookieSecure              bool
	SessionTTLMin             int
	WriteTimeout              time.Duration
	LoginFailureLimit         int
	CommentLimit              int
	PasswordResetLimit        int
	PasswordResetAccountLimit int
	TrustedProxyCIDRs         []string
}

func FromEnv() Settings {
	secure := os.Getenv("APP_COOKIE_SECURE") == "true"
	ttl := 60 * 24
	if raw := os.Getenv("APP_SESSION_TTL_MIN"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && int64(parsed) <= MaxSessionTTLMinutes {
			ttl = parsed
		}
	}
	loginFailureLimit := positiveEnv("APP_LOGIN_FAILURE_LIMIT", 5)
	commentLimit := positiveEnv("APP_COMMENT_RATE_LIMIT", 30)
	passwordResetLimit := positiveEnv("APP_PASSWORD_RESET_LIMIT", 5)
	passwordResetAccountLimit := positiveEnv("APP_PASSWORD_RESET_ACCOUNT_LIMIT", 1)
	trustedProxyCIDRs := commaSeparatedEnv("APP_TRUSTED_PROXIES")
	return Settings{
		Version:                   envOr("APP_VERSION", BuildVersion),
		ListenHost:                envOr("APP_LISTEN_HOST", "127.0.0.1"),
		CookieSecure:              secure,
		SessionTTLMin:             ttl,
		WriteTimeout:              positiveDurationEnv("APP_WRITE_TIMEOUT", 5*time.Minute),
		LoginFailureLimit:         loginFailureLimit,
		CommentLimit:              commentLimit,
		PasswordResetLimit:        passwordResetLimit,
		PasswordResetAccountLimit: passwordResetAccountLimit,
		TrustedProxyCIDRs:         trustedProxyCIDRs,
	}
}

func positiveDurationEnv(key string, fallback time.Duration) time.Duration {
	if raw := os.Getenv(key); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func commaSeparatedEnv(key string) []string {
	var values []string
	for _, value := range strings.Split(os.Getenv(key), ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func positiveEnv(key string, fallback int) int {
	if raw := os.Getenv(key); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
