package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func TestDecodeJSONRequiresExactSingleJSONContentType(t *testing.T) {
	for _, contentType := range []string{
		"application/json",
		"application/json; charset=utf-8",
		"Application/JSON",
	} {
		t.Run("accept "+contentType, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/", strings.NewReader(`{"ok":true}`))
			request.Header.Set("Content-Type", contentType)
			var body map[string]bool
			if err := decodeJSON(httptest.NewRecorder(), request, &body); err != nil {
				t.Fatalf("decodeJSON rejected %q: %v", contentType, err)
			}
			if !body["ok"] {
				t.Fatalf("decoded body = %#v", body)
			}
		})
	}

	for _, contentType := range []string{
		"",
		"text/application/json",
		"application/jsonp",
		"application/json-malformed",
		"application/json; charset",
	} {
		t.Run("reject "+contentType, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/", strings.NewReader(`{"ok":true}`))
			if contentType != "" {
				request.Header.Set("Content-Type", contentType)
			}
			var body map[string]bool
			if err := decodeJSON(httptest.NewRecorder(), request, &body); err == nil {
				t.Fatalf("decodeJSON accepted %q", contentType)
			}
		})
	}

	t.Run("reject duplicate content types", func(t *testing.T) {
		request := httptest.NewRequest("POST", "/", strings.NewReader(`{"ok":true}`))
		request.Header["Content-Type"] = []string{"application/json", "text/plain"}
		var body map[string]bool
		if err := decodeJSON(httptest.NewRecorder(), request, &body); err == nil {
			t.Fatal("decodeJSON accepted duplicate Content-Type headers")
		}
	})
}

func TestOversizedJSONReturnsRequestTooLarge(t *testing.T) {
	repo, err := store.NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	handler := NewServer(repo, config.Settings{}, nil).Handler()

	bodyText := `{"value":"` + strings.Repeat("x", maxSmallJSONBody) + `"}`
	request := httptest.NewRequest("POST", "/", strings.NewReader(bodyText))
	request.URL.Path = "/api/v1/setup"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusRequestEntityTooLarge || envelope.Error.Code != "request_too_large" {
		t.Fatalf("oversized body response = %d %#v", response.Code, envelope.Error)
	}
}
