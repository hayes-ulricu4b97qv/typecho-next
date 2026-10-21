package httpapi

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/service"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

const (
	maxForwardedForBytes      = 4096
	maxForwardedForHops       = 32
	postRevisionPageSizeLimit = 20
	forgotResponseFloor       = 250 * time.Millisecond
)

func (s *Server) loginFailureLimit() int {
	if s.settings.LoginFailureLimit > 0 {
		return s.settings.LoginFailureLimit
	}
	return 5
}

func (s *Server) commentRateLimit() int {
	if s.settings.CommentLimit > 0 {
		return s.settings.CommentLimit
	}
	return 30
}

func (s *Server) sessionTTL() time.Duration {
	minutes := s.settings.SessionTTLMin
	if minutes < 1 || int64(minutes) > config.MaxSessionTTLMinutes {
		minutes = 60 * 24
	}
	return time.Duration(minutes) * time.Minute
}

func (s *Server) allowComment(r *http.Request) bool {
	return s.commentHits.Allow(s.clientHost(r), s.commentRateLimit(), time.Minute)
}

func (s *Server) clientHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil {
		if host != "" {
			return host
		}
		return "unknown"
	}
	if !s.isTrustedProxy(peer) {
		return peer.String()
	}
	forwarded := strings.TrimSpace(strings.Join(r.Header.Values("X-Forwarded-For"), ","))
	if forwarded == "" {
		return peer.String()
	}
	if len(forwarded) > maxForwardedForBytes {
		return peer.String()
	}
	chain := strings.Split(forwarded, ",")
	if len(chain) > maxForwardedForHops {
		return peer.String()
	}
	addresses := make([]net.IP, 0, len(chain))
	for _, value := range chain {
		ip := net.ParseIP(strings.TrimSpace(value))
		if ip == nil {
			return peer.String()
		}
		addresses = append(addresses, ip)
	}
	for index := len(addresses) - 1; index >= 0; index-- {
		if !s.isTrustedProxy(addresses[index]) {
			return addresses[index].String()
		}
	}
	return addresses[0].String()
}

func parseTrustedProxyCIDRs(values []string, logger *slog.Logger) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err != nil {
			logger.Warn("ignoring invalid trusted proxy CIDR", "cidr", value, "error", err)
			continue
		}
		networks = append(networks, network)
	}
	return networks
}

func (s *Server) isTrustedProxy(ip net.IP) bool {
	for _, network := range s.trustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

type forgotRequest struct {
	Username string `json:"username"`
}

func (s *Server) forgotPassword(w http.ResponseWriter, r *http.Request) {
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
	var request forgotRequest
	if err := decodeAuthJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	limit := s.settings.PasswordResetLimit
	if limit < 1 {
		limit = 5
	}
	if !s.forgotHits.Allow(s.clientHost(r), limit, 15*time.Minute) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "请求过于频繁，请稍后再试")
		return
	}
	acceptedSince := time.Now()
	reservation, reserveErr := s.app.ReservePasswordReset(r.Context())
	if r.Context().Err() != nil {
		return
	}
	if errors.Is(reserveErr, service.ErrMailUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "mail_unavailable", "站点尚未配置安全邮件投递，暂时无法找回密码")
		return
	}
	if errors.Is(reserveErr, service.ErrPasswordResetBusy) {
		if waitForPasswordResetResponse(r.Context(), acceptedSince, nil, nil) {
			writeError(w, http.StatusServiceUnavailable, "password_reset_busy", "密码找回服务繁忙，请稍后重试")
		}
		return
	}
	if reserveErr != nil {
		s.logWarnRateLimited(r.Context(), "password-reset-reservation", "password reset capacity reservation failed", "error", reserveErr)
		if waitForPasswordResetResponse(r.Context(), acceptedSince, nil, nil) {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "密码找回服务暂不可用，请稍后重试")
		}
		return
	}
	defer reservation.Release()

	account, lookupErr := s.repo.UserContext(r.Context(), strings.TrimSpace(request.Username))
	if r.Context().Err() != nil {
		return
	}
	if lookupErr != nil && !errors.Is(lookupErr, store.ErrNotFound) {
		s.writeRepositoryUnavailable(w, r, lookupErr)
		return
	}
	accountKnown := lookupErr == nil && account.Active
	accountLimit := s.settings.PasswordResetAccountLimit
	if accountLimit < 1 {
		accountLimit = 1
	}
	if accountKnown && !s.forgotAccounts.Allow(account.Username, accountLimit, 15*time.Minute) {
		reservation.Release()
		if waitForPasswordResetResponse(r.Context(), acceptedSince, nil, nil) {
			writePasswordResetAccepted(w)
		}
		return
	}
	delivery, requestErr := reservation.RequestAsync(r.Context(), request.Username)
	if accountKnown && (requestErr != nil || delivery == nil) {
		// Do not cool down a known account when no reset email was queued.
		s.forgotAccounts.Reset(account.Username)
	}
	var deliveryFailed func()
	if accountKnown {
		deliveryFailed = func() { s.forgotAccounts.Reset(account.Username) }
	}
	if r.Context().Err() != nil {
		watchPasswordResetDelivery(delivery, deliveryFailed)
		return
	}
	if errors.Is(requestErr, service.ErrMailUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "mail_unavailable", "站点尚未配置安全邮件投递，暂时无法找回密码")
	} else {
		if requestErr != nil &&
			!errors.Is(requestErr, service.ErrPasswordResetBusy) &&
			!errors.Is(requestErr, service.ErrPasswordResetCleanup) &&
			s.forgotFailureLogs.Allow("request-failed", 1, time.Minute) {
			s.logger.Warn("password reset request failed", "error", requestErr)
		}
		if waitForPasswordResetResponse(r.Context(), acceptedSince, delivery, deliveryFailed) {
			writePasswordResetAccepted(w)
		}
	}
}

func waitForPasswordResetResponse(ctx context.Context, started time.Time, delivery <-chan error, onDeliveryFailure func()) bool {
	remaining := time.Until(started.Add(forgotResponseFloor))
	if remaining <= 0 {
		watchPasswordResetDelivery(delivery, onDeliveryFailure)
		return ctx.Err() == nil
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	if delivery != nil {
		select {
		case deliveryErr, ok := <-delivery:
			if ok && deliveryErr != nil && onDeliveryFailure != nil {
				onDeliveryFailure()
			}
		case <-timer.C:
			watchPasswordResetDelivery(delivery, onDeliveryFailure)
			return ctx.Err() == nil
		case <-ctx.Done():
			watchPasswordResetDelivery(delivery, onDeliveryFailure)
			return false
		}
	}
	select {
	case <-timer.C:
		return ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

func watchPasswordResetDelivery(delivery <-chan error, onDeliveryFailure func()) {
	if delivery == nil || onDeliveryFailure == nil {
		return
	}
	go func() {
		if deliveryErr, ok := <-delivery; ok && deliveryErr != nil {
			onDeliveryFailure()
		}
	}()
}

func writePasswordResetAccepted(w http.ResponseWriter) {
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "message": "如果账户存在，重置口令已发送到该账户的邮箱"})
}

type resetRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (s *Server) listOutboundMail(w http.ResponseWriter, r *http.Request) {
	if !s.requireOwner(w, r) {
		return
	}
	page, pageSize, err := pagination(r)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}
	mailPage, err := s.app.ListOutboundMailContext(r.Context(), page, pageSize)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "read_failed", "Unable to read mail")
		return
	}
	if mailPage.Items == nil {
		mailPage.Items = []store.OutboundMail{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": mailPage.Items, "total": mailPage.Total, "page": mailPage.Page, "pageSize": mailPage.PageSize})
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
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
	var request resetRequest
	if err := decodeAuthJSON(w, r, &request); err != nil {
		writeJSONDecodeError(w, r, err)
		return
	}
	if err := s.app.ResetPasswordContext(r.Context(), request.Token, request.Password); err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, service.ErrValidation) {
			writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "invalid_token", "The reset token is invalid or expired")
			return
		}
		if s.forgotFailureLogs.Allow("reset-failed", 1, time.Minute) {
			s.logger.Error("password reset failed", "error", err)
		}
		writeError(w, http.StatusInternalServerError, "reset_failed", "Unable to reset password")
		return
	}
	if r.Context().Err() != nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"reset": true})
}

func (s *Server) listPostRevisions(w http.ResponseWriter, r *http.Request) {
	if !s.requireContentEditor(w, r) {
		return
	}
	page, pageSize, ok := s.pages(w, r)
	if !ok {
		return
	}
	if pageSize > postRevisionPageSizeLimit {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", fmt.Sprintf("修订历史 pageSize 最大为 %d", postRevisionPageSizeLimit))
		return
	}
	revisions, err := s.app.ListPostRevisionPageContext(r.Context(), r.PathValue("id"), page, pageSize)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status, code := serviceError(err)
		if code == "not_found" {
			writeError(w, http.StatusNotFound, "not_found", "Post not found")
			return
		}
		writeError(w, status, "read_failed", "Unable to read revisions")
		return
	}
	if revisions.Items == nil {
		revisions.Items = []store.PostRevisionSummary{}
	}
	writeJSON(w, http.StatusOK, revisions)
}

func (s *Server) restorePostRevision(w http.ResponseWriter, r *http.Request) {
	if !s.requireContentEditor(w, r) {
		return
	}
	restored, changed, err := s.app.RestorePostRevisionWithChangeContext(r.Context(), r.PathValue("id"), r.PathValue("revisionID"))
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status, code := serviceError(err)
		if code == "not_found" {
			writeError(w, http.StatusNotFound, "not_found", "Revision not found")
			return
		}
		writeError(w, status, code, err.Error())
		return
	}
	if changed {
		s.recordAudit(r, "post.restored", "post", restored.ID, map[string]string{"revisionId": r.PathValue("revisionID")})
	}
	writeJSON(w, http.StatusOK, publicPost(restored, true))
}

func (s *Server) backup(w http.ResponseWriter, r *http.Request) {
	if !s.requireOwner(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	if !s.backupMu.TryLock() {
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, "backup_busy", "Another backup is already in progress")
		return
	}
	defer s.backupMu.Unlock()
	ctx := r.Context()
	tempDir, err := os.MkdirTemp("", "typecho-backup-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backup_failed", "Unable to prepare the backup")
		return
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			s.logWarnRateLimited(r.Context(), "backup-cleanup", "remove temporary backup files failed", "path", tempDir, "error", err)
		}
	}()
	dbPath := filepath.Join(tempDir, "app.db")
	if err := s.repo.BackupDatabaseContext(ctx, dbPath); err != nil {
		if ctx.Err() != nil {
			return
		}
		writeError(w, http.StatusInternalServerError, "backup_failed", "Unable to prepare the backup")
		return
	}
	archivePath := filepath.Join(tempDir, "backup.zip")
	if err := buildBackupContext(ctx, archivePath, dbPath, s.mediaRoot); err != nil {
		if ctx.Err() != nil {
			return
		}
		s.logErrorRateLimited(r.Context(), "backup-prepare", "prepare backup", "error", err)
		writeError(w, http.StatusInternalServerError, "backup_failed", "Unable to prepare the backup")
		return
	}
	file, err := os.Open(archivePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backup_failed", "Unable to prepare the backup")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "backup_failed", "Unable to prepare the backup")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="typecho-backup.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "typecho-backup.zip", info.ModTime(), file)
}

// Build the complete archive before sending a successful HTTP response. Media
// references come from the database snapshot, rather than the changing live DB.
func buildBackupContext(ctx context.Context, archivePath, dbPath, mediaRoot string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	absolutePath, err := filepath.Abs(dbPath)
	if err != nil {
		return err
	}
	databasePath := filepath.ToSlash(absolutePath)
	if !strings.HasPrefix(databasePath, "/") {
		databasePath = "/" + databasePath
	}
	databaseURL := url.URL{Scheme: "file", Path: databasePath, RawQuery: "mode=ro"}
	snapshot, err := sql.Open("sqlite3", databaseURL.String())
	if err != nil {
		return err
	}
	defer snapshot.Close()
	output, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := output.Close(); err == nil {
			err = closeErr
		}
	}()
	archive := zip.NewWriter(output)
	defer func() {
		if closeErr := archive.Close(); err == nil {
			err = closeErr
		}
	}()
	if err := addZipFileContext(ctx, archive, "app.db", dbPath); err != nil {
		return err
	}
	rows, err := snapshot.QueryContext(ctx, "SELECT id, storage_key FROM media ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			return err
		}
		if !validMediaStorageKey(key) {
			return fmt.Errorf("invalid media storage key: %q", key)
		}
		if err := addZipFileContext(ctx, archive, "media/"+key, filepath.Join(mediaRoot, key)); err != nil {
			return fmt.Errorf("backup media %s: %w", id, err)
		}
	}
	return rows.Err()
}

func addZipFile(archive *zip.Writer, name, path string) error {
	return addZipFileContext(context.Background(), archive, name, path)
}

func addZipFileContext(ctx context.Context, archive *zip.Writer, name, path string) error {
	file, info, err := openRegularFile(path)
	if err != nil {
		return err
	}
	defer file.Close()
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = name
	header.Method = zip.Deflate
	writer, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, contextReader{ctx: ctx, source: file})
	return err
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(buffer)
}

func openRegularFile(path string) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("file is not a regular file: %s", path)
	}
	pathInfo := info
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err = file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(pathInfo, info) {
		_ = file.Close()
		return nil, nil, fmt.Errorf("file changed while opening: %s", path)
	}
	return file, info, nil
}
