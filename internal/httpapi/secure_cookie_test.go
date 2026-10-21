package httpapi

import (
	"io"
	"log/slog"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func TestSecureCookieSettingAppliesToCSRFAndSessionCookies(t *testing.T) {
	repo, err := store.NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	handler := NewServer(repo, config.Settings{
		Version: "test", SessionTTLMin: 60, CookieSecure: true,
	}, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Jar = jar

	response, err := client.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	var csrfCookieFound bool
	for _, cookie := range response.Cookies() {
		if cookie.Name == csrfCookie {
			csrfCookieFound = true
			if !cookie.Secure || cookie.HttpOnly {
				t.Fatalf("CSRF cookie attributes = Secure:%v HttpOnly:%v, want Secure and script-readable", cookie.Secure, cookie.HttpOnly)
			}
		}
	}
	if !csrfCookieFound {
		t.Fatal("health response did not set the CSRF cookie")
	}

	setupSite(t, client, server.URL, "UTC")
	loginResponse := sendRaw(t, client, "POST", server.URL+"/api/v1/auth/login", map[string]string{
		"username": "owner", "password": "password1",
	}, 200)
	defer loginResponse.Body.Close()
	for _, cookie := range loginResponse.Cookies() {
		if cookie.Name == cookieName {
			if !cookie.Secure || !cookie.HttpOnly {
				t.Fatalf("session cookie attributes = Secure:%v HttpOnly:%v, want both enabled", cookie.Secure, cookie.HttpOnly)
			}
			return
		}
	}
	t.Fatal("login response did not issue the session cookie")
}
