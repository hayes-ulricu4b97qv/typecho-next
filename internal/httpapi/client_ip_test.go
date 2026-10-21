package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
)

func TestClientHostTrustsForwardedAddressOnlyFromConfiguredProxy(t *testing.T) {
	server := NewServer(nil, config.Settings{TrustedProxyCIDRs: []string{"10.0.0.0/8"}}, nil)
	tests := []struct {
		name           string
		remoteAddr     string
		forwarded      string
		forwardedLines []string
		want           string
	}{
		{name: "untrusted peer cannot spoof forwarded address", remoteAddr: "198.51.100.4:8080", forwarded: "203.0.113.8", want: "198.51.100.4"},
		{name: "walks past trusted proxy chain", remoteAddr: "10.0.0.2:8080", forwarded: "203.0.113.8, 10.0.0.3", want: "203.0.113.8"},
		{name: "joins duplicate forwarded headers in order", remoteAddr: "10.0.0.2:8080", forwardedLines: []string{"203.0.113.8", "198.51.100.20"}, want: "198.51.100.20"},
		{name: "invalid chain falls back to proxy", remoteAddr: "10.0.0.2:8080", forwarded: "203.0.113.8, invalid", want: "10.0.0.2"},
		{name: "oversized header falls back to proxy", remoteAddr: "10.0.0.2:8080", forwarded: strings.Repeat("x", maxForwardedForBytes+1), want: "10.0.0.2"},
		{name: "overlong chain falls back to proxy", remoteAddr: "10.0.0.2:8080", forwarded: strings.Repeat("203.0.113.8,", maxForwardedForHops), want: "10.0.0.2"},
		{name: "missing header uses proxy address", remoteAddr: "10.0.0.2:8080", want: "10.0.0.2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = test.remoteAddr
			if test.forwarded != "" {
				request.Header.Set("X-Forwarded-For", test.forwarded)
			}
			for _, value := range test.forwardedLines {
				request.Header.Add("X-Forwarded-For", value)
			}
			if got := server.clientHost(request); got != test.want {
				t.Fatalf("client address = %q, want %q", got, test.want)
			}
		})
	}
}
