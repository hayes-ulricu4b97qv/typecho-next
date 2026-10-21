package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

type loginCountingRepository struct {
	store.Repository
	userReads atomic.Int64
}

func (r *loginCountingRepository) User(username string) (store.User, error) {
	r.userReads.Add(1)
	return r.Repository.User(username)
}

func (r *loginCountingRepository) UserContext(ctx context.Context, username string) (store.User, error) {
	r.userReads.Add(1)
	return r.Repository.UserContext(ctx, username)
}

func TestLoginLimitIsPerClientAndRejectsBeforeAuthentication(t *testing.T) {
	repo, err := store.NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	countingRepo := &loginCountingRepository{Repository: repo}
	app := NewServer(countingRepo, config.Settings{Version: "test", SessionTTLMin: 60, LoginFailureLimit: 3}, nil)
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	setupSite(t, &http.Client{}, server.URL, "UTC")
	countingRepo.userReads.Store(0)

	remote := "192.0.2.10:5000"
	for _, username := range []string{"owner", "someone-else", "another-user"} {
		if got := directLogin(app, remote, username, "wrong-password").Code; got != http.StatusUnauthorized {
			t.Fatalf("failed login %q returned %d, want 401", username, got)
		}
	}
	if reads := countingRepo.userReads.Load(); reads != 3 {
		t.Fatalf("authentication reads after failed attempts = %d, want 3", reads)
	}
	if got := directLogin(app, remote, "owner", "password1").Code; got != http.StatusTooManyRequests {
		t.Fatalf("correct credentials from blocked client returned %d, want 429", got)
	}
	if reads := countingRepo.userReads.Load(); reads != 3 {
		t.Fatalf("blocked request reached authentication: user reads = %d", reads)
	}

	otherRemote := "198.51.100.8:5000"
	if got := directLogin(app, otherRemote, "owner", "wrong-password").Code; got != http.StatusUnauthorized {
		t.Fatalf("first other-client failure returned %d, want 401", got)
	}
	success := directLogin(app, otherRemote, "owner", "password1")
	if success.Code != http.StatusOK {
		t.Fatalf("valid login from another client returned %d, want 200", success.Code)
	}
	if len(success.Result().Cookies()) == 0 || success.Result().Cookies()[0].Name != cookieName {
		t.Fatalf("valid login did not issue the session cookie: %#v", success.Result().Cookies())
	}

	for index := 0; index < 4; index++ {
		want := http.StatusUnauthorized
		if index == 3 {
			want = http.StatusTooManyRequests
		}
		if got := directLogin(app, otherRemote, "owner", "wrong-password").Code; got != want {
			t.Fatalf("failed login after successful reset returned %d, want %d", got, want)
		}
	}
}

func TestOversizedLoginIsRejectedBeforeAuthentication(t *testing.T) {
	repo, err := store.NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	countingRepo := &loginCountingRepository{Repository: repo}
	server := NewServer(countingRepo, config.Settings{Version: "test"}, nil)
	if err := countingRepo.SetupContext(context.Background(),
		store.Site{Name: "Site", URL: "https://example.test", AdminMail: "owner@example.test", Timezone: "UTC"},
		store.Admin{Username: "owner", PasswordHash: "unused-hash", Email: "owner@example.test"},
	); err != nil {
		t.Fatal(err)
	}

	payload := `{"username":"owner","password":"` + strings.Repeat("x", maxAuthJSONBody) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "192.0.2.10:5000"
	response := httptest.NewRecorder()
	server.login(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized login status = %d, want %d: %s", response.Code, http.StatusRequestEntityTooLarge, response.Body.String())
	}
	if reads := countingRepo.userReads.Load(); reads != 0 {
		t.Fatalf("oversized login reached authentication: user reads = %d", reads)
	}
}

func directLogin(server *Server, remoteAddr, username, password string) *httptest.ResponseRecorder {
	payload, _ := json.Marshal(loginRequest{Username: username, Password: password})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = remoteAddr
	response := httptest.NewRecorder()
	server.login(response, request)
	return response
}
