package httpapi

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func TestBackupFailureAndRecovery(t *testing.T) {
	dir := t.TempDir()
	mediaRoot := filepath.Join(dir, "uploads")
	t.Setenv("APP_MEDIA_DIR", mediaRoot)
	repo, err := store.NewSQLiteRepository(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	server := httptest.NewServer(NewServer(repo, config.Settings{SessionTTLMin: 60}, nil).Handler())
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: jar}
	setupSite(t, owner, server.URL, "UTC")
	postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "Recover me", "slug": "recover-me", "content": "Saved body", "status": "published",
	}, http.StatusCreated)

	// A new site has no media directory and must still be backed up.
	getArchive := func() *zip.Reader {
		t.Helper()
		response, err := owner.Get(server.URL + "/api/v1/backup")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("backup status=%d headers=%v body=%s", response.StatusCode, response.Header, body)
		}
		archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatal(err)
		}
		return archive
	}
	if archive := getArchive(); len(archive.File) != 1 || archive.File[0].Name != "app.db" {
		t.Fatalf("empty site archive = %#v", archive.File)
	}
	_ = uploadGIF(t, owner, server.URL)
	media, err := repo.ListMedia(store.MediaQuery{Page: 1, PageSize: 100})
	if err != nil || len(media.Items) != 1 {
		t.Fatalf("media=%v err=%v", media, err)
	}
	mediaPath := filepath.Join(mediaRoot, media.Items[0].StorageKey)
	data, err := os.ReadFile(mediaPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(mediaPath); err != nil {
		t.Fatal(err)
	}
	failed := getStatus(t, owner, server.URL+"/api/v1/backup", http.StatusInternalServerError)
	if failed["error"].(map[string]any)["code"] != "backup_failed" {
		t.Fatalf("failure = %#v", failed)
	}
	if err := os.WriteFile(mediaPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// Unregistered temporary files must not be included in the backup.
	if err := os.WriteFile(filepath.Join(mediaRoot, ".upload-in-progress"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := getArchive()
	if len(archive.File) != 2 {
		t.Fatalf("archive files = %d", len(archive.File))
	}
	recoveryDir := t.TempDir()
	for _, entry := range archive.File {
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(recoveryDir, filepath.FromSlash(entry.Name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	recovered, err := store.NewSQLiteRepository(filepath.Join(recoveryDir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if !recovered.Initialized() {
		t.Fatal("recovered database is not initialized")
	}
	t.Setenv("APP_MEDIA_DIR", filepath.Join(recoveryDir, "media"))
	restoredServer := httptest.NewServer(NewServer(recovered, config.Settings{SessionTTLMin: 60}, nil).Handler())
	defer restoredServer.Close()
	post := get(t, owner, restoredServer.URL+"/api/v1/content/posts/recover-me")
	if post["content"] != "Saved body" {
		t.Fatalf("recovered post = %#v", post)
	}
	get(t, owner, restoredServer.URL+"/api/v1/auth/me")
	response, err := owner.Get(restoredServer.URL + "/media/" + media.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	restoredMedia, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || !bytes.Equal(restoredMedia, data) {
		t.Fatalf("recovered media status=%d err=%v", response.StatusCode, err)
	}
}

func TestAddZipFileDoesNotIncludeSymlinkTargets(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "outside.txt")
	link := filepath.Join(dir, "media-link")
	if err := os.WriteFile(target, []byte("outside data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links are not available: %v", err)
	}

	var archiveBytes bytes.Buffer
	archive := zip.NewWriter(&archiveBytes)
	if err := addZipFile(archive, "media/media-link", link); err == nil {
		t.Fatal("included a symbolic-link target in the backup")
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archiveBytes.Bytes()), int64(archiveBytes.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 0 {
		t.Fatalf("archive contains entries after the symbolic-link rejection: %#v", reader.File)
	}
}

func TestServeMediaDoesNotFollowSymlinks(t *testing.T) {
	dir := t.TempDir()
	mediaRoot := filepath.Join(dir, "uploads")
	t.Setenv("APP_MEDIA_DIR", mediaRoot)
	repo, err := store.NewSQLiteRepository(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	server := httptest.NewServer(NewServer(repo, config.Settings{SessionTTLMin: 60}, nil).Handler())
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: jar}
	setupSite(t, owner, server.URL, "UTC")
	upload := uploadGIF(t, owner, server.URL)
	media, err := repo.Media(upload["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(mediaRoot, media.StorageKey)
	target := filepath.Join(dir, "private.txt")
	if err := os.WriteFile(target, []byte("private host data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(mediaPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, mediaPath); err != nil {
		t.Skipf("symbolic links are not available: %v", err)
	}

	response, err := http.Get(server.URL + "/media/" + media.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusInternalServerError || strings.Contains(string(body), "private host data") {
		t.Fatalf("symlink media response status=%d body=%q", response.StatusCode, body)
	}
}
