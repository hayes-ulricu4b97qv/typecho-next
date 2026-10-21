package httpapi

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/url"
	"testing"
)

func TestOversizedMultipartUsesTypedBodyLimit(t *testing.T) {
	_, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "large.gif")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte{'x'}, maxUploadSize+(1<<20)+1)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var csrf string
	for _, cookie := range owner.Jar.Cookies(baseURL) {
		if cookie.Name == csrfCookie {
			csrf = cookie.Value
			break
		}
	}
	if csrf == "" {
		t.Fatal("setup did not issue a CSRF cookie")
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/media", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set(csrfHeader, csrf)
	response, err := owner.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	result := decodeBody(t, response, http.StatusRequestEntityTooLarge)
	apiError, ok := result["error"].(map[string]any)
	if !ok || apiError["code"] != "file_too_large" {
		t.Fatalf("oversized upload response = %#v, want file_too_large", result)
	}
}
