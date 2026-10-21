package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func TestPaginationRejectsInvalidPagesAndOffsetOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	tests := []struct {
		name string
		page string
	}{
		{name: "zero", page: "0"},
		{name: "negative", page: "-1"},
		{name: "not an integer", page: "first"},
		{name: "overflowing offset", page: strconv.Itoa(maxInt)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/?page="+test.page+"&pageSize=100", nil)
			if _, _, err := pagination(request); err == nil {
				t.Fatalf("pagination accepted page=%q", test.page)
			}
		})
	}
}

func TestPaginationAcceptsLargestNonOverflowingOffset(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	pageSize := 100
	page := maxInt/pageSize + 1
	request := httptest.NewRequest("GET", "/?page="+strconv.Itoa(page)+"&pageSize=100", nil)
	gotPage, gotPageSize, err := pagination(request)
	if err != nil {
		t.Fatalf("pagination rejected largest safe offset: %v", err)
	}
	offset := (gotPage - 1) * gotPageSize
	if gotPage != page || gotPageSize != pageSize || offset < 0 || offset > maxInt {
		t.Fatalf("page=%d pageSize=%d offset=%d, want safe offset for page %d", gotPage, gotPageSize, offset, page)
	}
}

func TestPostListingReturnsValidationErrorForInvalidPage(t *testing.T) {
	_, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")
	maxInt := strconv.Itoa(int(^uint(0) >> 1))
	for _, page := range []string{"0", maxInt} {
		response := getStatus(t, owner, server.URL+"/api/v1/content/posts?page="+page+"&pageSize=100", 422)
		if response["error"].(map[string]any)["code"] != "validation_error" {
			t.Fatalf("page=%s error = %#v", page, response["error"])
		}
	}
}

func TestAPIScalarQueryParametersRejectDuplicatesAndMalformedEscapes(t *testing.T) {
	repo, err := store.NewSQLiteRepository(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	handler := NewServer(repo, config.Settings{}, nil).Handler()
	for _, key := range scalarAPIQueryParameters {
		t.Run("duplicate "+key, func(t *testing.T) {
			queryKey := url.QueryEscape(key)
			request := httptest.NewRequest("GET", "/api/v1/content/posts?"+queryKey+"=first&"+queryKey+"=second", nil)
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
			if response.Code != http.StatusUnprocessableEntity || envelope.Error.Code != "validation_error" {
				t.Fatalf("duplicate query %q response = %d %#v", key, response.Code, envelope.Error)
			}
		})
	}
	for _, key := range []string{"includeDraft", "includePending"} {
		for _, value := range []string{"", "yes", "TRUE", "1"} {
			t.Run("invalid boolean "+key+"="+value, func(t *testing.T) {
				request := httptest.NewRequest("GET", "/api/v1/content/posts?"+url.QueryEscape(key)+"="+url.QueryEscape(value), nil)
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
				if response.Code != http.StatusUnprocessableEntity || envelope.Error.Code != "validation_error" {
					t.Fatalf("invalid boolean %s=%q response = %d %#v", key, value, response.Code, envelope.Error)
				}
			})
		}
	}
	for _, key := range []string{"includeDraft", "includePending"} {
		for _, value := range []string{"true", "false"} {
			request := httptest.NewRequest("GET", "/api/v1/system/status?"+url.QueryEscape(key)+"="+value, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("valid boolean %s=%s returned %d: %s", key, value, response.Code, response.Body.String())
			}
		}
	}

	request := httptest.NewRequest("GET", "/api/v1/content/posts?q=%ZZ", nil)
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
	if response.Code != http.StatusBadRequest || envelope.Error.Code != "invalid_request" {
		t.Fatalf("malformed query response = %d %#v", response.Code, envelope.Error)
	}
}
