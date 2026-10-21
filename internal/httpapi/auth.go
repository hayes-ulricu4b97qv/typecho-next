package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/service"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

type setupRequest struct {
	SiteName      string `json:"siteName"`
	SiteURL       string `json:"siteUrl"`
	AdminUsername string `json:"adminUsername"`
	AdminPassword string `json:"adminPassword"`
	AdminEmail    string `json:"adminEmail"`
	Timezone      string `json:"timezone"`
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.repo.InitializedContext(r.Context())
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		s.writeRepositoryUnavailable(w, r, err)
		return
	}
	if initialized {
		writeError(w, http.StatusConflict, "already_initialized", "The application has already been initialized")
		return
	}
	var request setupRequest
	if err := decodeAuthJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	setup, err := s.app.InitializeContext(r.Context(), service.SetupInput{
		SiteName:      request.SiteName,
		SiteURL:       request.SiteURL,
		AdminUsername: request.AdminUsername,
		AdminPassword: request.AdminPassword,
		AdminEmail:    request.AdminEmail,
		Timezone:      request.Timezone,
	})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		switch {
		case errors.Is(err, store.ErrAlreadyInitialized):
			writeError(w, http.StatusConflict, "already_initialized", "The application has already been initialized")
		case errors.Is(err, service.ErrValidation):
			writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		case errors.Is(err, service.ErrRepositoryUnavailable):
			s.writeRepositoryUnavailable(w, r, err)
		default:
			writeError(w, http.StatusInternalServerError, "setup_failed", "Unable to complete initial setup")
		}
		return
	}
	if err := s.issueSessionContext(r.Context(), w, request.AdminUsername); err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, service.ErrRepositoryUnavailable) {
			s.writeRepositoryUnavailable(w, r, err)
		} else {
			writeError(w, http.StatusInternalServerError, "session_failed", "Unable to create a sign-in session")
		}
		return
	}
	// The setup request has no session cookie yet, so keep the actor identity in details.
	s.recordAudit(r, "site.initialized", "site", "1", map[string]string{"owner": request.AdminUsername})
	writeJSON(w, http.StatusCreated, map[string]any{"initialized": true, "site": publicSite(setup)})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.repo.InitializedContext(r.Context())
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		s.writeRepositoryUnavailable(w, r, err)
		return
	}
	if !initialized {
		writeError(w, http.StatusPreconditionRequired, "setup_required", "Complete the initial setup at /setting")
		return
	}
	var request loginRequest
	if err := decodeAuthJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	key := s.clientHost(r)
	window := 10 * time.Minute
	limit := s.loginFailureLimit()
	if !s.loginFailures.Allow(key, limit, window) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "尝试过于频繁，请稍后再试")
		return
	}
	admin, err := s.app.AuthenticateContext(r.Context(), request.Username, request.Password)
	if err != nil && r.Context().Err() != nil {
		return
	}
	if err != nil && !errors.Is(err, service.ErrInvalidCredentials) {
		if errors.Is(err, service.ErrRepositoryUnavailable) {
			s.writeRepositoryUnavailable(w, r, err)
		} else {
			writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read administrator account")
		}
		return
	}
	if errors.Is(err, service.ErrInvalidCredentials) || admin == nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Username or password is incorrect")
		return
	}
	s.loginFailures.Reset(key)
	if err := s.issueAuthenticatedSessionContext(r.Context(), w, admin.Username, admin.PasswordHash); err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "Username or password is incorrect")
			return
		}
		if errors.Is(err, service.ErrRepositoryUnavailable) {
			s.writeRepositoryUnavailable(w, r, err)
		} else {
			writeError(w, http.StatusInternalServerError, "session_failed", "Unable to create a sign-in session")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": publicUser(admin)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	var revokeErr error
	if cookie, err := r.Cookie(cookieName); err == nil {
		revokeErr = s.app.RevokeSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.settings.CookieSecure})
	if revokeErr != nil {
		s.logErrorRateLimited(r.Context(), "logout-session-revoke", "unable to revoke sign-in session during logout", "error", revokeErr)
		writeError(w, http.StatusInternalServerError, "session_revoke_failed", "Unable to revoke sign-in session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	admin, err := s.authenticatedAdmin(r.Context(), r)
	if r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue")
		return
	}
	if err != nil {
		s.writeRepositoryUnavailable(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": publicUser(admin)})
}
