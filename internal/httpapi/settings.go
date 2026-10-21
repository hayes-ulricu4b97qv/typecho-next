package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/service"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

func (s *Server) dashboardStats(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	stats, err := s.app.StatsContext(r.Context())
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read post stats")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) getSite(w http.ResponseWriter, r *http.Request) {
	if !s.requireOwner(w, r) {
		return
	}
	site, err := s.app.SiteContext(r.Context())
	if err != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) || site == nil {
		writeError(w, http.StatusNotFound, "not_found", "Site settings not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read site settings")
		return
	}
	writeJSON(w, http.StatusOK, publicSite(site))
}

func (s *Server) updateSite(w http.ResponseWriter, r *http.Request) {
	if !s.requireOwner(w, r) {
		return
	}
	var input struct {
		Name        string `json:"name"`
		URL         string `json:"url"`
		AdminMail   string `json:"adminMail"`
		Timezone    string `json:"timezone"`
		Description string `json:"description"`
		Keywords    string `json:"keywords"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	updated, changed, err := s.app.UpdateSiteWithChangeContext(r.Context(), store.Site{Name: input.Name, URL: input.URL, AdminMail: input.AdminMail, Timezone: input.Timezone, Description: input.Description, Keywords: input.Keywords})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Site settings not found")
			return
		}
		status, code := serviceError(err)
		writeError(w, status, code, err.Error())
		return
	}
	if changed {
		s.recordAudit(r, "site.updated", "site", "1", nil)
	}
	writeJSON(w, http.StatusOK, publicSite(updated))
}

func (s *Server) getReading(w http.ResponseWriter, r *http.Request) {
	if !s.requireOwner(w, r) {
		return
	}
	site, err := s.app.SiteContext(r.Context())
	if err != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) || site == nil {
		writeError(w, http.StatusNotFound, "not_found", "Site settings not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read reading settings")
		return
	}
	writeJSON(w, http.StatusOK, publicReading(site))
}

func (s *Server) updateReading(w http.ResponseWriter, r *http.Request) {
	if !s.requireOwner(w, r) {
		return
	}
	var input struct {
		PostsPerPage              int  `json:"postsPerPage"`
		CommentsEnabled           bool `json:"commentsEnabled"`
		CommentsRequireModeration bool `json:"commentsRequireModeration"`
		CommentsThreaded          bool `json:"commentsThreaded"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	updated, changed, err := s.app.UpdateReadingWithChangeContext(r.Context(), service.ReadingInput{
		PostsPerPage:              input.PostsPerPage,
		CommentsEnabled:           input.CommentsEnabled,
		CommentsRequireModeration: input.CommentsRequireModeration,
		CommentsThreaded:          input.CommentsThreaded,
	})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Site settings not found")
			return
		}
		status, code := serviceError(err)
		writeError(w, status, code, err.Error())
		return
	}
	if changed {
		s.recordAudit(r, "site.reading_updated", "site", "1", map[string]string{
			"postsPerPage":      strconv.Itoa(updated.PostsPerPage),
			"commentsEnabled":   strconv.FormatBool(updated.CommentsEnabled),
			"requireModeration": strconv.FormatBool(updated.CommentsRequireModeration),
			"threaded":          strconv.FormatBool(updated.CommentsThreaded),
		})
	}
	writeJSON(w, http.StatusOK, publicReading(updated))
}

type accountRequest struct {
	Email           string `json:"email"`
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requireOwner(w, r) {
		return
	}
	var input accountRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	admin, changed, passwordChanged, err := s.app.UpdateAdminAccountWithChangeContext(r.Context(), service.AccountInput{
		Email:           input.Email,
		CurrentPassword: input.CurrentPassword,
		NewPassword:     input.NewPassword,
		SessionToken:    sessionToken(r),
	})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "Current password is incorrect")
		case errors.Is(err, service.ErrValidation):
			writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Administrator account not found")
		case errors.Is(err, store.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "Administrator account changed during this request")
		case errors.Is(err, store.ErrStaleWrite):
			writeError(w, http.StatusConflict, "conflict", "Administrator account changed during this request")
		default:
			writeError(w, http.StatusInternalServerError, "save_failed", "Unable to update administrator account")
		}
		return
	}
	if changed {
		s.recordAudit(r, "account.updated", "user", admin.Username, map[string]string{"passwordChanged": strconv.FormatBool(passwordChanged)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": publicUser(admin)})
}

type userRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Active   *bool  `json:"active"`
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireOwner(w, r) {
		return
	}
	users, err := s.app.ListUsersContext(r.Context())
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read users")
		return
	}
	items := make([]userResponse, 0, len(users))
	for _, user := range users {
		items = append(items, publicUserRecord(user))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireOwner(w, r) {
		return
	}
	var input userRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	user, err := s.app.CreateUserContext(r.Context(), service.UserInput{Username: input.Username, Email: input.Email, Password: input.Password, Role: input.Role})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "username_taken", "用户名已被使用")
			return
		}
		status, code := serviceError(err)
		writeError(w, status, code, err.Error())
		return
	}
	s.recordAudit(r, "user.created", "user", user.ID, map[string]string{"username": user.Username, "role": user.Role})
	writeJSON(w, http.StatusCreated, publicUserRecord(*user))
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireOwner(w, r) {
		return
	}
	var input userRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	user, changed, err := s.app.UpdateUserWithChangeContext(r.Context(), r.PathValue("username"), service.UserInput{Email: input.Email, Password: input.Password, Role: input.Role, Active: input.Active})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status, code := serviceError(err)
		writeError(w, status, code, err.Error())
		return
	}
	if changed {
		s.recordAudit(r, "user.updated", "user", user.ID, map[string]string{"username": user.Username, "role": user.Role, "active": strconv.FormatBool(user.Active)})
	}
	writeJSON(w, http.StatusOK, publicUserRecord(*user))
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireOwner(w, r) {
		return
	}
	err := s.app.DeleteUserContext(r.Context(), r.PathValue("username"))
	if err != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		status, code := serviceError(err)
		writeError(w, status, code, err.Error())
		return
	}
	s.recordAudit(r, "user.deleted", "user", r.PathValue("username"), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	if !s.requireOwner(w, r) {
		return
	}
	page, pageSize, ok := s.pages(w, r)
	if !ok {
		return
	}
	events, err := s.app.ListAuditContext(r.Context(), r.URL.Query().Get("action"), r.URL.Query().Get("resourceType"), page, pageSize)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read audit events")
		return
	}
	writeJSON(w, http.StatusOK, events)
}
