package httpapi

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIActionsDisableCaching(t *testing.T) {
	_, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")

	for _, path := range []string{
		"/api/v1/system/status",
		"/api/v1/auth/me",
		"/api/v1/settings/site",
		"/api/v1/audit",
		"/api/v1/mail",
	} {
		response, err := owner.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("GET %s Cache-Control = %q, want no-store", path, response.Header.Get("Cache-Control"))
		}
	}
}

func TestRenderedPrivateContentDisablesCaching(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte(`<html><body><div id="app"></div></body></html>`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_WEB_DIR", webDir)
	_, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")
	postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "Private draft", "slug": "private-draft", "content": "CACHE_PRIVATE_BODY", "status": "draft",
	}, http.StatusCreated)

	response, err := owner.Get(server.URL + "/user/private-draft?preview=true")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "CACHE_PRIVATE_BODY") {
		t.Fatalf("private preview response status=%d body=%q", response.StatusCode, body)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("private preview Cache-Control = %q, want no-store", response.Header.Get("Cache-Control"))
	}
}

func TestStaticAssetsCacheAccordingToFilenameVersioning(t *testing.T) {
	webDir := t.TempDir()
	assetsDir := filepath.Join(webDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"index-AbCd1234.js":     "hashed bundle",
		"app-CNc-d_0-.js":       "URL-safe hashed bundle",
		"photo-cover-image.svg": "unversioned asset with dash-separated name",
		"logo.svg":              "unversioned asset",
	} {
		if err := os.WriteFile(filepath.Join(assetsDir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(webDir, "favicon.svg"), []byte("favicon"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_WEB_DIR", webDir)
	_, server, _ := newAPIServer(t)
	client := &http.Client{}

	for _, test := range []struct {
		path        string
		cachePolicy string
	}{
		{"/assets/index-AbCd1234.js", "public, max-age=31536000, immutable"},
		{"/assets/app-CNc-d_0-.js", "public, max-age=31536000, immutable"},
		{"/assets/photo-cover-image.svg", "no-cache"},
		{"/assets/logo.svg", "no-cache"},
		{"/favicon.svg", "no-cache"},
	} {
		response, err := client.Get(server.URL + test.path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != test.cachePolicy {
			t.Errorf("GET %s status=%d Cache-Control=%q, want %q", test.path, response.StatusCode, response.Header.Get("Cache-Control"), test.cachePolicy)
		}
		if response.Header.Get("Set-Cookie") != "" {
			t.Errorf("GET %s unexpectedly set a cookie on a cacheable static response: %q", test.path, response.Header.Get("Set-Cookie"))
		}
	}
}

func TestImmutablePublicMediaDoesNotSetCSRFCookie(t *testing.T) {
	_, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")
	media := uploadGIF(t, owner, server.URL)

	response, err := http.Get(server.URL + media["url"].(string))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("media response status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if response.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("media Cache-Control = %q", response.Header.Get("Cache-Control"))
	}
	if response.Header.Get("Set-Cookie") != "" {
		t.Fatalf("cacheable media response unexpectedly set a cookie: %q", response.Header.Get("Set-Cookie"))
	}
}
