package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAPIFallbackErrorsUseJSONEnvelope(t *testing.T) {
	_, server, client := newAPIServer(t)
	setupSite(t, client, server.URL, "UTC")

	unknown, err := client.Get(server.URL + "/api/v1/does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	defer unknown.Body.Close()
	assertAPIFallbackError(t, unknown, http.StatusNotFound, "not_found")

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/system/status", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	methodMismatch, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer methodMismatch.Body.Close()
	assertAPIFallbackError(t, methodMismatch, http.StatusMethodNotAllowed, "method_not_allowed")
	if !strings.Contains(methodMismatch.Header.Get("Allow"), http.MethodGet) {
		t.Fatalf("method mismatch Allow header = %q, want GET", methodMismatch.Header.Get("Allow"))
	}
}

func assertAPIFallbackError(t *testing.T, response *http.Response, status int, code string) {
	t.Helper()
	if response.StatusCode != status {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("API fallback status = %d, want %d: %s", response.StatusCode, status, body)
	}
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("API fallback content type = %q, body=%q; want JSON", response.Header.Get("Content-Type"), body)
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode API fallback JSON: %v", err)
	}
	if payload.Error.Code != code || !containsHan(payload.Error.Message) {
		t.Fatalf("API fallback error = %#v, want code %q with localized message", payload.Error, code)
	}
}
