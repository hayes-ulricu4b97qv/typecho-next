package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/service"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func TestSearchQueryLengthIsBoundedForContentEndpoints(t *testing.T) {
	repo, err := store.NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	app := NewServer(repo, config.Settings{Version: "test", SessionTTLMin: 60}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	setupSite(t, client, server.URL, "UTC")

	endpoints := []string{
		"/api/v1/content/posts",
		"/api/v1/content/pages",
		"/api/v1/content/terms",
	}
	withinLimit := url.QueryEscape(strings.Repeat("界", service.MaxSearchRunes))
	tooLong := url.QueryEscape(strings.Repeat("界", service.MaxSearchRunes+1))
	for _, endpoint := range endpoints {
		t.Run(strings.TrimPrefix(endpoint, "/api/v1/content/"), func(t *testing.T) {
			base := server.URL + endpoint + "?page=1&pageSize=1&q="
			getStatus(t, client, base+withinLimit, http.StatusOK)
			response := getStatus(t, client, base+tooLong, http.StatusUnprocessableEntity)
			errorBody, ok := response["error"].(map[string]any)
			if !ok || errorBody["code"] != "validation_error" {
				t.Fatalf("overlong search response = %#v, want validation_error", response)
			}
		})
	}
}
