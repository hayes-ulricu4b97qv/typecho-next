package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestBcryptCredentialInputsRejectMoreThan72Bytes(t *testing.T) {
	_, server, owner := newAPIServer(t)
	get(t, owner, server.URL+"/api/v1/system/status")

	tooLong := strings.Repeat("p", 73)
	setup := map[string]string{
		"siteName": "Next", "siteUrl": "https://example.com", "adminUsername": "owner",
		"adminPassword": tooLong, "adminEmail": "owner@example.com", "timezone": "UTC",
	}
	invalidSetup := postJSON(t, owner, server.URL+"/api/v1/setup", setup, http.StatusUnprocessableEntity)
	if invalidSetup["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("overlong setup password = %#v", invalidSetup)
	}
	setup["adminPassword"] = strings.Repeat("p", 72)
	postJSON(t, owner, server.URL+"/api/v1/setup", setup, http.StatusCreated)

	invalidAccount := putJSON(t, owner, server.URL+"/api/v1/settings/account", map[string]string{
		"email": "owner@example.com", "currentPassword": setup["adminPassword"], "newPassword": tooLong,
	}, http.StatusUnprocessableEntity)
	if invalidAccount["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("overlong account password = %#v", invalidAccount)
	}

	invalidUser := postJSON(t, owner, server.URL+"/api/v1/settings/users", map[string]string{
		"username": "editor1", "email": "editor@example.com", "password": tooLong, "role": "editor",
	}, http.StatusUnprocessableEntity)
	if invalidUser["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("overlong new-user password = %#v", invalidUser)
	}
	created := postJSON(t, owner, server.URL+"/api/v1/settings/users", map[string]string{
		"username": "editor1", "email": "editor@example.com", "password": "password1", "role": "editor",
	}, http.StatusCreated)
	invalidUserUpdate := putJSON(t, owner, server.URL+"/api/v1/settings/users/"+created["username"].(string), map[string]string{
		"password": tooLong,
	}, http.StatusUnprocessableEntity)
	if invalidUserUpdate["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("overlong updated-user password = %#v", invalidUserUpdate)
	}

	invalidPassphrase := postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]string{
		"title": "口令边界", "slug": "passphrase-limit", "content": "正文", "status": "draft", "passphrase": tooLong,
	}, http.StatusUnprocessableEntity)
	if invalidPassphrase["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("overlong reading passphrase = %#v", invalidPassphrase)
	}
}
