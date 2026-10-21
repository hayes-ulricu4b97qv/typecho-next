package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteErrorUsesChineseFallbackForUnmappedEnglishMessage(t *testing.T) {
	response := httptest.NewRecorder()
	writeError(response, http.StatusInternalServerError, "read_failed", "read posts: missing column custom_fields")

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusInternalServerError || body.Error.Code != "read_failed" || body.Error.Message != "读取失败，请稍后再试" {
		t.Fatalf("localized response = status %d, %#v", response.Code, body.Error)
	}
}

func TestWriteErrorKeepsMappedMessageAndMachineCode(t *testing.T) {
	response := httptest.NewRecorder()
	writeError(response, http.StatusForbidden, "forbidden", "Owner permission is required")

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusForbidden || body.Error.Code != "forbidden" || body.Error.Message != "需要所有者权限" {
		t.Fatalf("localized response = status %d, %#v", response.Code, body.Error)
	}
}
