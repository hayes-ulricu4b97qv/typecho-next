package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

type rejectFirstMediaDeleteRepository struct {
	store.Repository
	reject bool
}

func (r *rejectFirstMediaDeleteRepository) DeleteMedia(id string) error {
	if r.reject {
		r.reject = false
		return errors.New("injected media delete failure")
	}
	return r.Repository.DeleteMedia(id)
}

func TestDeleteMediaRestoresFileWhenRepositoryDeleteFails(t *testing.T) {
	dir := t.TempDir()
	mediaRoot := filepath.Join(dir, "uploads")
	t.Setenv("APP_MEDIA_DIR", mediaRoot)
	repo, err := store.NewSQLiteRepository(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	wrapped := &rejectFirstMediaDeleteRepository{Repository: repo, reject: true}
	server := httptest.NewServer(NewServer(wrapped, config.Settings{SessionTTLMin: 60}, nil).Handler())
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := &http.Client{Jar: jar}
	setupSite(t, owner, server.URL, "UTC")
	upload := uploadGIF(t, owner, server.URL)
	mediaID := upload["id"].(string)
	media, err := repo.Media(mediaID)
	if err != nil {
		t.Fatal(err)
	}
	originalPath := filepath.Join(mediaRoot, media.StorageKey)
	originalBytes, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	stagedPath := mediaDeleteStagingPath(mediaRoot, mediaID)

	deleteMedia := func() *http.Response {
		t.Helper()
		request, err := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/media/"+mediaID, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, cookie := range owner.Jar.Cookies(request.URL) {
			if cookie.Name == csrfCookie {
				request.Header.Set(csrfHeader, cookie.Value)
			}
		}
		response, err := owner.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	response := deleteMedia()
	var failure struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusInternalServerError || failure.Error.Code != "delete_failed" {
		t.Fatalf("failed delete response = status %d, error %#v", response.StatusCode, failure.Error)
	}
	if restored, err := os.ReadFile(originalPath); err != nil || !bytes.Equal(restored, originalBytes) {
		t.Fatalf("original media after rollback = %v, err = %v", restored, err)
	}
	if _, err := os.Lstat(stagedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged media after rollback: err = %v, want not-exist", err)
	}
	if _, err := repo.Media(mediaID); err != nil {
		t.Fatalf("media metadata after failed delete: %v", err)
	}
	deletedAudit, err := repo.ListAudit(store.AuditQuery{Action: "media.deleted", ResourceType: "media", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(deletedAudit.Items) != 0 {
		t.Fatalf("failed delete wrote audit events: %#v", deletedAudit.Items)
	}

	response = deleteMedia()
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("retry delete status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	if _, err := repo.Media(mediaID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("media metadata after retry: %v, want not found", err)
	}
	if _, err := os.Lstat(originalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("original media after retry: err = %v, want not-exist", err)
	}
	deletedAudit, err = repo.ListAudit(store.AuditQuery{Action: "media.deleted", ResourceType: "media", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(deletedAudit.Items) != 1 {
		t.Fatalf("successful retry audit events = %d, want 1", len(deletedAudit.Items))
	}
}
