package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

type cancelSessionCreateRepository struct {
	store.Repository
	cancel       context.CancelFunc
	createdToken chan string
	revoked      chan struct{}
}

func (r *cancelSessionCreateRepository) CreateSessionContext(ctx context.Context, username, tokenHash string, expiresAt time.Time) error {
	if err := r.Repository.CreateSessionContext(ctx, username, tokenHash, expiresAt); err != nil {
		return err
	}
	r.createdToken <- tokenHash
	r.cancel()
	return nil
}

func (r *cancelSessionCreateRepository) DeleteSession(tokenHash string) error {
	err := r.Repository.DeleteSession(tokenHash)
	close(r.revoked)
	return err
}

type cancelMediaDeleteRepository struct {
	store.Repository
	cancel  context.CancelFunc
	deleted chan struct{}
}

func (r *cancelMediaDeleteRepository) DeleteMedia(id string) error {
	err := r.Repository.DeleteMedia(id)
	r.cancel()
	close(r.deleted)
	return err
}

func TestLogoutRevokesSession(t *testing.T) {
	_, server, client := newAPIServer(t)
	setupSite(t, client, server.URL, "UTC")
	var sessionValue string
	for _, cookie := range client.Jar.Cookies(mustParseURL(t, server.URL)) {
		if cookie.Name == cookieName {
			sessionValue = cookie.Value
			break
		}
	}
	if sessionValue == "" {
		t.Fatal("setup did not issue a session cookie")
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/auth/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	setTestCSRFHeader(client, request)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}

	request, err = http.NewRequest(http.MethodGet, server.URL+"/api/v1/auth/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: cookieName, Value: sessionValue})
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session after logout returned %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}
}

func TestCanceledSessionIssuanceRevokesUnissuedSession(t *testing.T) {
	repo, err := store.NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	wrapped := &cancelSessionCreateRepository{
		Repository:   repo,
		createdToken: make(chan string, 1),
		revoked:      make(chan struct{}),
	}
	server := NewServer(wrapped, config.Settings{Version: "test", SessionTTLMin: 60}, nil)
	if err := repo.SetupContext(context.Background(),
		store.Site{Name: "Next", URL: "https://example.test", AdminMail: "owner@example.test", Timezone: "UTC"},
		store.Admin{Username: "owner", PasswordHash: "unused-hash", Email: "owner@example.test"},
	); err != nil {
		t.Fatal(err)
	}

	requestContext, cancel := context.WithCancel(context.Background())
	wrapped.cancel = cancel
	defer cancel()
	response := httptest.NewRecorder()
	if err := server.issueSessionContext(requestContext, response, "owner"); !errors.Is(err, context.Canceled) {
		t.Fatalf("session issuance error = %v, want context canceled", err)
	}
	if cookie := response.Header().Get("Set-Cookie"); cookie != "" {
		t.Fatalf("canceled session issuance set a cookie: %q", cookie)
	}

	var tokenHash string
	select {
	case tokenHash = <-wrapped.createdToken:
	case <-time.After(2 * time.Second):
		t.Fatal("session issuance did not persist a session before cancellation")
	}
	select {
	case <-wrapped.revoked:
	case <-time.After(2 * time.Second):
		t.Fatal("unissued session was not revoked after cancellation")
	}
	if _, err := repo.ResolveSessionContext(context.Background(), tokenHash, time.Now().UTC()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("session after canceled issuance error = %v, want not found", err)
	}
}

func TestCanceledMediaDeleteFinishesCleanupAndAudit(t *testing.T) {
	dir := t.TempDir()
	mediaRoot := filepath.Join(dir, "uploads")
	t.Setenv("APP_MEDIA_DIR", mediaRoot)
	repo, err := store.NewSQLiteRepository(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	wrapped := &cancelMediaDeleteRepository{Repository: repo, deleted: make(chan struct{})}
	server, client := newContextBoundaryServer(t, wrapped)
	setupSite(t, client, server.URL, "UTC")
	mediaResult := uploadGIF(t, client, server.URL)
	media, err := repo.Media(mediaResult["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	originalPath := filepath.Join(mediaRoot, media.StorageKey)

	requestContext, cancel := context.WithCancel(context.Background())
	wrapped.cancel = cancel
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodDelete, server.URL+"/api/v1/media/"+media.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	setTestCSRFHeader(client, request)
	if response, err := client.Do(request); response != nil {
		response.Body.Close()
	} else if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled media delete request error = %v", err)
	}
	select {
	case <-wrapped.deleted:
	case <-time.After(2 * time.Second):
		t.Fatal("media delete did not reach its consistency boundary")
	}

	stagedPath := mediaDeleteStagingPath(mediaRoot, media.ID)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, mediaErr := repo.Media(media.ID)
		_, originalErr := os.Stat(originalPath)
		_, stagedErr := os.Stat(stagedPath)
		audit, auditErr := repo.ListAudit(store.AuditQuery{Action: "media.deleted", ResourceType: "media", Page: 1, PageSize: 10})
		if errors.Is(mediaErr, store.ErrNotFound) && errors.Is(originalErr, os.ErrNotExist) && errors.Is(stagedErr, os.ErrNotExist) && auditErr == nil && len(audit.Items) == 1 {
			if audit.Items[0].ActorUsername != "owner" {
				t.Fatalf("media delete audit actor = %q, want owner", audit.Items[0].ActorUsername)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("delete cleanup incomplete: mediaErr=%v originalErr=%v stagedErr=%v audit=%#v auditErr=%v", mediaErr, originalErr, stagedErr, audit.Items, auditErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newContextBoundaryServer(t *testing.T, repo store.Repository) (*httptest.Server, *http.Client) {
	t.Helper()
	server := httptest.NewServer(NewServer(repo, config.Settings{Version: "test", SessionTTLMin: 60}, nil).Handler())
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return server, &http.Client{Jar: jar}
}

func setTestCSRFHeader(client *http.Client, request *http.Request) {
	if client.Jar == nil {
		return
	}
	for _, cookie := range client.Jar.Cookies(request.URL) {
		if cookie.Name == csrfCookie {
			request.Header.Set(csrfHeader, cookie.Value)
			return
		}
	}
}

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
