package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestForgotPasswordAccountLimitAppliesAcrossSources(t *testing.T) {
	mailer := &recordingMailer{}
	_, server, owner := newAPIServer(t, mailer)
	setupSite(t, owner, server.URL, "UTC")

	first := directForgotRequest(t, server.Config.Handler, "192.0.2.10:1234", "owner")
	second := directForgotRequest(t, server.Config.Handler, "198.51.100.20:2345", "owner")
	unknown := directForgotRequest(t, server.Config.Handler, "198.51.100.20:2345", "missing-user")
	assertSameForgotShape(t, first, second)
	assertSameForgotShape(t, first, unknown)
	if messages := mailer.snapshot(); len(messages) != 1 {
		t.Fatalf("account recovery emails = %d, want 1", len(messages))
	}
}

func TestForgotPasswordAccountLimitAllowsRetryAfterDeliveryFailure(t *testing.T) {
	mailer := &recordingMailer{err: errors.New("SMTP unavailable")}
	_, server, owner := newAPIServer(t, mailer)
	setupSite(t, owner, server.URL, "UTC")

	failed := directForgotRequest(t, server.Config.Handler, "192.0.2.30:1234", "owner")
	mailer.err = nil
	retried := directForgotRequest(t, server.Config.Handler, "198.51.100.30:2345", "owner")
	assertSameForgotShape(t, failed, retried)
	if messages := mailer.snapshot(); len(messages) != 2 {
		t.Fatalf("recovery attempts = %d, want failed attempt and successful retry", len(messages))
	}
}

func directForgotRequest(t *testing.T, handler http.Handler, remoteAddr, username string) map[string]any {
	t.Helper()
	status, result := directForgotStatus(t, handler, remoteAddr, username)
	if status != http.StatusAccepted {
		t.Fatalf("forgot response status = %d, want %d", status, http.StatusAccepted)
	}
	return result
}

func directForgotStatus(t *testing.T, handler http.Handler, remoteAddr, username string) (int, map[string]any) {
	t.Helper()
	payload, err := json.Marshal(forgotRequest{Username: username})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/forgot", bytes.NewReader(payload))
	request.RemoteAddr = remoteAddr
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeader, "csrf-test-token")
	request.AddCookie(&http.Cookie{Name: csrfCookie, Value: "csrf-test-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return response.Code, result
}
