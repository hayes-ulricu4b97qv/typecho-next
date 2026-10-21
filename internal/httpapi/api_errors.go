package httpapi

import (
	"mime"
	"net/http"
	"strings"
)

func apiErrorEnvelope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isVersionedAPIPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(&apiErrorWriter{ResponseWriter: w}, r)
	})
}

func isVersionedAPIPath(path string) bool {
	return path == "/api/v1" || strings.HasPrefix(path, "/api/v1/")
}

type apiErrorWriter struct {
	http.ResponseWriter
	wrote    bool
	replaced bool
}

func (w *apiErrorWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *apiErrorWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.wrote = true
	mediaType, _, mediaTypeErr := mime.ParseMediaType(w.Header().Get("Content-Type"))
	isJSON := mediaTypeErr == nil && strings.EqualFold(mediaType, "application/json")
	if (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) &&
		!isJSON {
		code, message := "not_found", "Not found"
		if status == http.StatusMethodNotAllowed {
			code, message = "method_not_allowed", "Method not allowed"
		}
		w.Header().Del("Content-Length")
		w.replaced = true
		writeError(w.ResponseWriter, status, code, message)
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *apiErrorWriter) Write(payload []byte) (int, error) {
	if w.replaced {
		return len(payload), nil
	}
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(payload)
}
