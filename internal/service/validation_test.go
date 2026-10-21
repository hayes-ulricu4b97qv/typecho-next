package service

import (
	"strings"
	"testing"
)

func TestValidSiteURLPortBounds(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "default port", url: "https://example.com", want: true},
		{name: "case insensitive scheme", url: "HTTPS://example.com", want: true},
		{name: "IPv6 without port", url: "https://[::1]", want: true},
		{name: "minimum port", url: "https://example.com:1", want: true},
		{name: "maximum port", url: "https://example.com:65535", want: true},
		{name: "empty explicit port", url: "https://example.com:", want: false},
		{name: "empty explicit IPv6 port", url: "https://[::1]:", want: false},
		{name: "zero port", url: "https://example.com:0", want: false},
		{name: "port above maximum", url: "https://example.com:65536", want: false},
		{name: "nonnumeric port", url: "https://example.com:abc", want: false},
		{name: "port without hostname", url: "https://:80", want: false},
		{name: "empty query delimiter", url: "https://example.com?", want: false},
		{name: "empty fragment delimiter", url: "https://example.com#", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validSiteURL(test.url); got != test.want {
				t.Errorf("validSiteURL(%q) = %t, want %t", test.url, got, test.want)
			}
		})
	}
}

func TestValidURLRequiresUsableHTTPAuthority(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "https host", url: "https://example.com/path?q=x#section", want: true},
		{name: "case insensitive scheme", url: "HTTPS://example.com", want: true},
		{name: "IPv6 host", url: "http://[::1]:8080/path", want: true},
		{name: "empty hostname", url: "https://:80", want: false},
		{name: "empty explicit port", url: "https://example.com:", want: false},
		{name: "zero port", url: "https://example.com:0", want: false},
		{name: "port above maximum", url: "https://example.com:65536", want: false},
		{name: "nonnumeric port", url: "https://example.com:abc", want: false},
		{name: "userinfo", url: "https://user:password@example.com", want: false},
		{name: "host-like userinfo", url: "https://trusted.example@attacker.example", want: false},
		{name: "non HTTP scheme", url: "javascript:alert(1)", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validURL(test.url); got != test.want {
				t.Errorf("validURL(%q) = %t, want %t", test.url, got, test.want)
			}
		})
	}
}

func TestNormalizeCustomFieldsTrimsKeysAndRejectsCollisions(t *testing.T) {
	fields, err := normalizeCustomFields(map[string]string{" title ": "value"})
	if err != nil {
		t.Fatalf("normalizeCustomFields() error = %v", err)
	}
	if got := fields["title"]; got != "value" {
		t.Fatalf("normalized title = %q, want value", got)
	}
	if _, err := normalizeCustomFields(map[string]string{"title": "one", " title ": "two"}); err == nil {
		t.Fatal("normalizeCustomFields() accepted keys that collide after trimming")
	}
}

func TestNormalizeCustomFieldsCountsUnicodeRunesAtLimits(t *testing.T) {
	if _, err := normalizeCustomFields(map[string]string{strings.Repeat("中", 80): strings.Repeat("中", 2000)}); err != nil {
		t.Fatalf("normalizeCustomFields() rejected values at rune limits: %v", err)
	}
	if _, err := normalizeCustomFields(map[string]string{strings.Repeat("中", 81): "value"}); err == nil {
		t.Fatal("normalizeCustomFields() accepted a key above the rune limit")
	}
	if _, err := normalizeCustomFields(map[string]string{"key": strings.Repeat("中", 2001)}); err == nil {
		t.Fatal("normalizeCustomFields() accepted a value above the rune limit")
	}
}

func TestValidateBcryptInputCountsUTF8Bytes(t *testing.T) {
	if err := validateBcryptInput(strings.Repeat("中", 24), "password"); err != nil {
		t.Fatalf("validateBcryptInput() rejected 72 UTF-8 bytes: %v", err)
	}
	if err := validateBcryptInput(strings.Repeat("中", 25), "password"); err == nil {
		t.Fatal("validateBcryptInput() accepted 75 UTF-8 bytes")
	}
}
