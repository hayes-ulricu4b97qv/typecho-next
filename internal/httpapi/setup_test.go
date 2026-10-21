package httpapi

import (
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func TestIgnoredLegacyFileAndUserSetup(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	legacyPath := filepath.Join(dir, "app.json")
	if err := os.WriteFile(legacyPath, []byte(`{
		"initialized": true,
		"site": {"name": "Legacy Site", "url": "https://legacy.example", "adminMail": "legacy@example.com", "timezone": "UTC"},
		"admin": {"username": "legacy", "passwordHash": "not-a-user", "email": "legacy@example.com"},
		"posts": [{"id": "legacy-post", "title": "Old", "slug": "legacy-post", "content": "old body", "status": "published"}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	repo, err := store.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Initialized() {
		t.Fatal("opening a new database loaded the legacy file")
	}
	if _, err := repo.Site(); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("legacy site was loaded: %v", err)
	}
	posts, err := repo.Posts()
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 0 {
		t.Fatalf("legacy posts were loaded: %#v", posts)
	}
	if _, err := repo.User("legacy"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("legacy admin was copied into users: %v", err)
	}

	handler := NewServer(repo, config.Settings{Version: "test", SessionTTLMin: 60}, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	server := httptest.NewServer(handler)
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	root, err := noRedirect.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	root.Body.Close()
	if root.StatusCode != http.StatusTemporaryRedirect || root.Header.Get("Location") != "/setting" {
		t.Fatalf("GET / = %d Location %q", root.StatusCode, root.Header.Get("Location"))
	}
	health, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	healthBody := decodeBody(t, health, http.StatusOK)
	if healthBody["status"] != "ok" || healthBody["version"] != "test" {
		t.Fatalf("health = %#v", healthBody)
	}
	ready, err := http.Get(server.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	readyBody := decodeBody(t, ready, http.StatusServiceUnavailable)
	if readyBody["error"].(map[string]any)["code"] != "not_ready" {
		t.Fatalf("ready = %#v", readyBody)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := &http.Client{Jar: jar}
	get(t, owner, server.URL+"/api/v1/system/status")
	invalidSetup := postJSON(t, owner, server.URL+"/api/v1/setup", map[string]string{
		"siteName": "Next", "siteUrl": "https://example.com/?campaign=test", "adminUsername": "owner",
		"adminPassword": "password1", "adminEmail": "owner@example.com", "timezone": "UTC",
	}, http.StatusUnprocessableEntity)
	if invalidSetup["error"].(map[string]any)["code"] != "validation_error" || repo.Initialized() {
		t.Fatalf("setup accepted a site URL with a query or partially initialized the database: %#v", invalidSetup)
	}
	configured := postJSON(t, owner, server.URL+"/api/v1/setup", map[string]string{
		"siteName": "Next", "siteUrl": "https://example.com", "adminUsername": "owner",
		"adminPassword": "password1", "adminEmail": "owner@example.com", "timezone": " UTC ",
	}, http.StatusCreated)
	if configured["site"].(map[string]any)["timezone"] != "UTC" {
		t.Fatalf("setup did not normalize timezone: %#v", configured["site"])
	}

	sessionURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var sessionValue string
	for _, cookie := range jar.Cookies(sessionURL) {
		if cookie.Name == cookieName {
			sessionValue = cookie.Value
		}
	}
	if sessionValue == "" {
		t.Fatal("setup did not create a session cookie")
	}
	server.Close()
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if !reopened.Initialized() {
		t.Fatal("setup did not persist an initialized store")
	}
	again := httptest.NewServer(NewServer(reopened, config.Settings{Version: "test", SessionTTLMin: 60}, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer again.Close()

	againJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	againURL, err := url.Parse(again.URL)
	if err != nil {
		t.Fatal(err)
	}
	againJar.SetCookies(againURL, []*http.Cookie{{Name: cookieName, Value: sessionValue, Path: "/"}})
	resumed := &http.Client{Jar: againJar}
	me := get(t, resumed, again.URL+"/api/v1/auth/me")
	user := me["user"].(map[string]any)
	if user["username"] != "owner" || user["email"] != "owner@example.com" || user["role"] != "owner" {
		t.Fatalf("resumed session = %#v", me)
	}

	updated := putJSON(t, resumed, again.URL+"/api/v1/settings/account", map[string]string{
		"email": "owner-next@example.com", "currentPassword": "password1", "newPassword": "password2",
	}, http.StatusOK)
	updatedUser := updated["user"].(map[string]any)
	if updatedUser["email"] != "owner-next@example.com" || updatedUser["username"] != "owner" {
		t.Fatalf("account update = %#v", updated)
	}
	still := get(t, resumed, again.URL+"/api/v1/auth/me")
	if still["user"].(map[string]any)["email"] != "owner-next@example.com" {
		t.Fatalf("current session was not kept after the password change: %#v", still)
	}

	signedIn := loginAs(t, again.URL, "owner", "password2")
	confirmed := get(t, signedIn, again.URL+"/api/v1/auth/me")
	if confirmed["user"].(map[string]any)["email"] != "owner-next@example.com" {
		t.Fatalf("login after account update = %#v", confirmed)
	}
	rejected := postJSON(t, &http.Client{}, again.URL+"/api/v1/auth/login", map[string]string{
		"username": "owner", "password": "password1",
	}, http.StatusUnauthorized)
	if rejected["error"].(map[string]any)["code"] != "invalid_credentials" {
		t.Fatalf("old password was still accepted: %#v", rejected)
	}

	again.Close()
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	assertSetupRows(t, dbPath)
}

func assertSetupRows(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(dbPath)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var admins, users, legacyPosts int
	var username, email, role, siteName string
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'admins'`).Scan(&admins); err != nil {
		t.Fatal(err)
	}
	if admins != 0 {
		t.Fatalf("new database still creates an admins table")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != 1 {
		t.Fatalf("users = %d, want the owner created by setup", users)
	}
	if err := db.QueryRow(`SELECT username, email, role FROM users`).Scan(&username, &email, &role); err != nil {
		t.Fatal(err)
	}
	if username != "owner" || email != "owner-next@example.com" || role != "owner" {
		t.Fatalf("owner row = %s %s %s", username, email, role)
	}
	if err := db.QueryRow(`SELECT name FROM sites WHERE id = 1`).Scan(&siteName); err != nil {
		t.Fatal(err)
	}
	if siteName != "Next" {
		t.Fatalf("site name = %s", siteName)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM posts WHERE id = 'legacy-post' OR slug = 'legacy-post'`).Scan(&legacyPosts); err != nil {
		t.Fatal(err)
	}
	if legacyPosts != 0 {
		t.Fatalf("legacy posts = %d", legacyPosts)
	}
}
