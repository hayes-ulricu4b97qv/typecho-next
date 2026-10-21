package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
	"golang.org/x/crypto/bcrypt"
)

var ErrMailUnavailable = errors.New("SMTP mail is not configured")

// ErrPasswordResetBusy reports that all bounded reset-email delivery slots are in use.
var ErrPasswordResetBusy = errors.New("password reset delivery is busy")

// ErrPasswordResetCleanup reports that an undispatched reset token could not be removed.
var ErrPasswordResetCleanup = errors.New("password reset token cleanup failed")

const (
	mailDeliveryTimeout             = 10 * time.Second
	passwordResetCleanupTimeout     = time.Second
	passwordResetBusyLogInterval    = time.Minute
	passwordResetFailureLogInterval = time.Minute
)

// PasswordResetReservation holds a bounded delivery slot while an HTTP request
// checks its account cooldown and prepares a reset email.
type PasswordResetReservation struct {
	service *Service
	mu      sync.Mutex
	state   uint8
}

const (
	passwordResetReservationHeld uint8 = iota
	passwordResetReservationProcessing
	passwordResetReservationTransferred
	passwordResetReservationReleased
)

func (s *Service) ListOutboundMail(page, pageSize int) (store.OutboundMailPage, error) {
	return s.repo.ListOutboundMail(page, pageSize)
}

func (s *Service) ListOutboundMailContext(ctx context.Context, page, pageSize int) (store.OutboundMailPage, error) {
	return s.repo.ListOutboundMailContext(ctx, page, pageSize)
}

// MailAvailable reports whether the service has a configured mailer.
func (s *Service) MailAvailable() bool {
	return s.mailer != nil
}

func (s *Service) notifyOwner(ctx context.Context, site *store.Site, subject, body string) {
	if site == nil {
		return
	}
	recipient := strings.TrimSpace(site.AdminMail)
	if recipient == "" {
		return
	}
	if err := s.repo.SaveOutboundMailContext(ctx, recipient, subject, body); err != nil {
		if ctx.Err() == nil && s.ownerNotificationFailureLogs.Allow("save", 1, time.Minute) {
			s.logger.Warn("save owner notification failed", "subject", subject, "error", err)
		}
	}
	if s.mailer != nil {
		mailCtx, cancel := context.WithTimeout(ctx, mailDeliveryTimeout)
		deliveryErr := s.mailer.Send(mailCtx, recipient, subject, body)
		cancel()
		if deliveryErr != nil && ctx.Err() == nil && s.ownerNotificationFailureLogs.Allow("deliver", 1, time.Minute) {
			s.logger.Warn("deliver owner notification failed", "subject", subject, "error", deliveryErr)
		}
	}
}

func (s *Service) RequestPasswordReset(ctx context.Context, username string) error {
	delivery, err := s.RequestPasswordResetAsync(ctx, username)
	if err != nil || delivery == nil {
		return err
	}
	select {
	case deliveryErr := <-delivery:
		return deliveryErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RequestPasswordResetAsync stores the one-time token before dispatching email
// delivery in the background. The returned channel reports delivery completion.
func (s *Service) RequestPasswordResetAsync(ctx context.Context, username string) (<-chan error, error) {
	reservation, err := s.ReservePasswordReset(ctx)
	if err != nil {
		return nil, err
	}
	defer reservation.Release()
	return reservation.RequestAsync(ctx, username)
}

// ReservePasswordReset acquires bounded capacity before account-specific work.
func (s *Service) ReservePasswordReset(ctx context.Context) (*PasswordResetReservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.mailer == nil {
		return nil, ErrMailUnavailable
	}
	select {
	case s.passwordResetSlots <- struct{}{}:
	default:
		s.logPasswordResetCapacity()
		return nil, ErrPasswordResetBusy
	}
	if !s.beginBackgroundWork() {
		<-s.passwordResetSlots
		return nil, ErrPasswordResetBusy
	}
	reservation := &PasswordResetReservation{service: s}
	if err := ctx.Err(); err != nil {
		reservation.Release()
		return nil, err
	}
	return reservation, nil
}

// RequestAsync prepares a reset token and transfers its reservation to the
// background delivery worker once the email is ready to send.
func (r *PasswordResetReservation) RequestAsync(ctx context.Context, username string) (<-chan error, error) {
	if r == nil || r.service == nil || !r.begin() {
		return nil, fmt.Errorf("password reset reservation is no longer available")
	}
	s := r.service
	transferred := false
	defer func() {
		if !transferred {
			r.complete()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes)
	tokenHash := store.HashResetToken(token)
	expires := time.Now().UTC().Add(30 * time.Minute)
	user, err := s.repo.CreatePasswordResetForActiveUserContext(ctx, strings.TrimSpace(username), tokenHash, expires)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	site, err := s.repo.SiteContext(ctx)
	if err != nil {
		return nil, s.discardUndispatchedPasswordReset(tokenHash, err)
	}
	resetURL := "/admin"
	if site != nil && strings.TrimSpace(site.URL) != "" {
		resetURL = strings.TrimRight(strings.TrimSpace(site.URL), "/") + "/admin"
	}
	body := fmt.Sprintf("你好 %s，\n\n请在 30 分钟内使用下面的重置口令设置新密码：\n重置口令：%s\n\n管理页面：%s\n如果这不是你发起的请求，请忽略本邮件。\n", user.Username, token, resetURL)
	if err := ctx.Err(); err != nil {
		return nil, s.discardUndispatchedPasswordReset(tokenHash, err)
	}
	delivery := make(chan error, 1)
	if !r.transfer() {
		transferErr := fmt.Errorf("password reset reservation was released before delivery")
		return nil, s.discardUndispatchedPasswordReset(tokenHash, transferErr)
	}
	transferred = true
	go func() {
		defer r.complete()
		mailContext, cancel := context.WithTimeout(context.Background(), mailDeliveryTimeout)
		deliveryErr := s.mailer.Send(mailContext, user.Email, "重置 Typecho Next 密码", body)
		cancel()
		if deliveryErr != nil {
			deliveryErr = fmt.Errorf("send reset email: %w", deliveryErr)
			if cleanupErr := s.deletePasswordResetWithTimeout(tokenHash); cleanupErr != nil {
				deliveryErr = errors.Join(deliveryErr, cleanupErr)
			}
			s.logPasswordResetDeliveryFailure(deliveryErr)
		}
		delivery <- deliveryErr
		close(delivery)
	}()
	return delivery, nil
}

func (r *PasswordResetReservation) begin() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != passwordResetReservationHeld {
		return false
	}
	r.state = passwordResetReservationProcessing
	return true
}

func (r *PasswordResetReservation) transfer() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != passwordResetReservationProcessing {
		return false
	}
	r.state = passwordResetReservationTransferred
	return true
}

// Release returns an unused reservation to the service.
func (r *PasswordResetReservation) Release() {
	if r == nil || r.service == nil {
		return
	}
	r.mu.Lock()
	if r.state != passwordResetReservationHeld {
		r.mu.Unlock()
		return
	}
	r.state = passwordResetReservationReleased
	r.mu.Unlock()
	r.releaseResources()
}

func (r *PasswordResetReservation) complete() {
	r.mu.Lock()
	if r.state == passwordResetReservationReleased {
		r.mu.Unlock()
		return
	}
	r.state = passwordResetReservationReleased
	r.mu.Unlock()
	r.releaseResources()
}

func (r *PasswordResetReservation) releaseResources() {
	<-r.service.passwordResetSlots
	r.service.backgroundWork.Done()
}

func (s *Service) deletePasswordResetWithTimeout(tokenHash string) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), passwordResetCleanupTimeout)
	defer cancel()
	return s.repo.DeletePasswordResetContext(cleanupCtx, tokenHash)
}

func (s *Service) discardUndispatchedPasswordReset(tokenHash string, cause error) error {
	cleanupErr := s.deletePasswordResetWithTimeout(tokenHash)
	if cleanupErr == nil {
		return cause
	}
	s.logPasswordResetCleanupFailure(cause, cleanupErr)
	cleanupFailure := fmt.Errorf("%w: %w", ErrPasswordResetCleanup, cleanupErr)
	return errors.Join(cause, cleanupFailure)
}

func (s *Service) logPasswordResetDeliveryFailure(err error) {
	suppressed, ok := s.claimPasswordResetFailureLog()
	if !ok {
		return
	}
	args := []any{"error", err}
	if suppressed > 0 {
		args = append(args, "suppressed", suppressed)
	}
	s.logger.Warn("password reset email delivery failed", args...)
}

func (s *Service) logPasswordResetCleanupFailure(cause, cleanupErr error) {
	suppressed, ok := s.claimPasswordResetFailureLog()
	if !ok {
		return
	}
	args := []any{"cause", cause, "error", cleanupErr}
	if suppressed > 0 {
		args = append(args, "suppressed", suppressed)
	}
	s.logger.Error("unable to discard undispatched password reset", args...)
}

func (s *Service) claimPasswordResetFailureLog() (uint64, bool) {
	now := time.Now()
	s.passwordResetFailureLogMu.Lock()
	defer s.passwordResetFailureLogMu.Unlock()
	if now.Sub(s.lastPasswordResetFailureLog) < passwordResetFailureLogInterval {
		s.passwordResetFailureLogsSuppressed++
		return 0, false
	}
	suppressed := s.passwordResetFailureLogsSuppressed
	s.passwordResetFailureLogsSuppressed = 0
	s.lastPasswordResetFailureLog = now
	return suppressed, true
}

func (s *Service) logPasswordResetCapacity() {
	now := time.Now()
	s.passwordResetLogMu.Lock()
	if now.Sub(s.lastPasswordResetLog) < passwordResetBusyLogInterval {
		s.passwordResetLogMu.Unlock()
		return
	}
	s.lastPasswordResetLog = now
	s.passwordResetLogMu.Unlock()

	s.logger.Warn("password reset delivery capacity reached", "limit", cap(s.passwordResetSlots))
}

func (s *Service) beginBackgroundWork() bool {
	s.backgroundWorkMu.Lock()
	defer s.backgroundWorkMu.Unlock()
	if s.backgroundWorkClosing {
		return false
	}
	s.backgroundWork.Add(1)
	return true
}

// WaitForBackgroundWork rejects new background reset-email jobs and waits for active jobs.
func (s *Service) WaitForBackgroundWork(ctx context.Context) error {
	s.backgroundWorkMu.Lock()
	s.backgroundWorkClosing = true
	s.backgroundWorkMu.Unlock()

	done := make(chan struct{})
	go func() {
		s.backgroundWork.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) ResetPassword(token, newPassword string) error {
	return s.ResetPasswordContext(context.Background(), token, newPassword)
}

func (s *Service) ResetPasswordContext(ctx context.Context, token, newPassword string) error {
	if len(newPassword) < 8 {
		return fmt.Errorf("%w: 密码至少 8 个字符", ErrValidation)
	}
	if err := validateBcryptInput(newPassword, "密码"); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tokenHash := store.HashResetToken(strings.TrimSpace(token))
	if err := s.repo.CheckPasswordResetContext(ctx, tokenHash, time.Now().UTC()); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.repo.CompletePasswordReset(ctx, tokenHash, time.Now().UTC(), string(hash))
}
