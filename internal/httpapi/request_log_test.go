package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLogReplacesSuppliedIDAndLogsFinalResponse(t *testing.T) {
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logOutput, nil))
	var contextID string
	handler := requestLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextID = requestIDFromContext(r.Context())
		if contextID == "" || contextID == r.Header.Get("X-Request-ID") {
			t.Errorf("context request ID = %q, caller-supplied ID = %q", contextID, r.Header.Get("X-Request-ID"))
		}
		w.WriteHeader(http.StatusCreated)
	}), logger)

	request := httptest.NewRequest(http.MethodGet, "/request-id", nil)
	request.Header.Set("X-Request-ID", "caller-controlled")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	requestID := response.Header().Get("X-Request-ID")
	if requestID == "" || requestID == "caller-controlled" || requestID != contextID {
		t.Fatalf("response ID = %q, context ID = %q; expected a matching server-generated ID", requestID, contextID)
	}
	entry := decodeRequestLog(t, logOutput.Bytes())
	if entry["request_id"] != requestID {
		t.Fatalf("logged request ID = %v, want %q", entry["request_id"], requestID)
	}
	if entry["status"] != float64(http.StatusCreated) || entry["level"] != "INFO" {
		t.Fatalf("logged status/level = %v/%v, want 201/INFO", entry["status"], entry["level"])
	}
}

func TestRequestLogKeepsFinalStatusAfterInformationalResponses(t *testing.T) {
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logOutput, nil))
	response := &statusCaptureWriter{header: make(http.Header)}
	handler := requestLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted"))
	}), logger)

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/hints", nil))
	if want := []int{http.StatusEarlyHints, http.StatusEarlyHints, http.StatusAccepted}; !equalStatuses(response.statuses, want) {
		t.Fatalf("written statuses = %v, want %v", response.statuses, want)
	}
	entry := decodeRequestLog(t, logOutput.Bytes())
	if entry["status"] != float64(http.StatusAccepted) {
		t.Fatalf("logged status = %v, want %d", entry["status"], http.StatusAccepted)
	}
}

func TestRequestLogFlushThroughResponseControllerRecordsImplicitSuccess(t *testing.T) {
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logOutput, nil))
	response := &statusCaptureWriter{header: make(http.Header)}
	handler := requestLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("ResponseController.Flush() error = %v", err)
		}
	}), logger)

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/flush", nil))
	if response.flushes != 1 {
		t.Fatalf("flush count = %d, want 1", response.flushes)
	}
	entry := decodeRequestLog(t, logOutput.Bytes())
	if entry["status"] != float64(http.StatusOK) {
		t.Fatalf("logged status after implicit flush = %v, want 200", entry["status"])
	}
}

func TestRequestLogPanicOmitsPanicValueAndLogsStack(t *testing.T) {
	const privatePanicValue = "sensitive-panic-value"
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logOutput, nil))
	handler := requestLog(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(privatePanicValue)
	}), logger)
	response := httptest.NewRecorder()
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	}()

	if !errors.Is(asError(recovered), http.ErrAbortHandler) {
		t.Fatalf("recovered panic = %v, want http.ErrAbortHandler", recovered)
	}
	output := logOutput.String()
	if strings.Contains(output, privatePanicValue) {
		t.Fatalf("panic value leaked to log: %s", output)
	}
	entry := decodeRequestLog(t, logOutput.Bytes())
	if entry["level"] != "ERROR" || entry["status"] != float64(http.StatusInternalServerError) || entry["panicked"] != true {
		t.Fatalf("panic log fields = %#v", entry)
	}
	if stack, _ := entry["stack"].(string); !strings.Contains(stack, "request_log_test.go") {
		t.Fatalf("panic log stack does not identify the test handler: %q", stack)
	}
}

type statusCaptureWriter struct {
	header   http.Header
	statuses []int
	flushes  int
	body     bytes.Buffer
}

func (w *statusCaptureWriter) Header() http.Header { return w.header }

func (w *statusCaptureWriter) WriteHeader(status int) {
	w.statuses = append(w.statuses, status)
}

func (w *statusCaptureWriter) Write(payload []byte) (int, error) {
	if len(w.statuses) == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(payload)
}

func (w *statusCaptureWriter) Flush() {
	if len(w.statuses) == 0 {
		w.WriteHeader(http.StatusOK)
	}
	w.flushes++
}

func decodeRequestLog(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(payload), &entry); err != nil {
		t.Fatalf("decode request log: %v; payload %s", err, payload)
	}
	return entry
}

func equalStatuses(actual, expected []int) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func asError(value any) error {
	err, _ := value.(error)
	return err
}
