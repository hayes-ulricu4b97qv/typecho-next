package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type sentMail struct {
	to      string
	subject string
	body    string
}

type recordingMailer struct {
	mu       sync.Mutex
	messages []sentMail
	err      error
}

func (m *recordingMailer) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentMail{to: to, subject: subject, body: body})
	return m.err
}

func (m *recordingMailer) snapshot() []sentMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sentMail(nil), m.messages...)
}

func TestPasswordResetIsDeliveredToAccountEmailAndOneTime(t *testing.T) {
	mailer := &recordingMailer{}
	repo, server, owner := newAPIServer(t, mailer)
	setupSite(t, owner, server.URL, "UTC")
	if err := repo.SaveOutboundMail("owner@example.com", "旧通知", "更早的一封站内通知"); err != nil {
		t.Fatal(err)
	}
	postJSON(t, owner, server.URL+"/api/v1/settings/users", map[string]string{
		"username": "editor1", "email": "editor@example.com", "password": "password1", "role": "editor",
	}, http.StatusCreated)

	public := cookieClient(t)
	get(t, public, server.URL+"/api/v1/system/status")
	unknown := postJSON(t, public, server.URL+"/api/v1/auth/forgot", map[string]string{"username": "missing-user"}, http.StatusAccepted)
	known := postJSON(t, public, server.URL+"/api/v1/auth/forgot", map[string]string{"username": "owner"}, http.StatusAccepted)
	assertSameForgotShape(t, unknown, known)

	messages := mailer.snapshot()
	if len(messages) != 1 || messages[0].to != "owner@example.com" || messages[0].subject != "重置 Typecho Next 密码" {
		t.Fatalf("password reset email = %#v", messages)
	}
	token := resetToken(messages[0].body)
	if len(token) != 64 {
		t.Fatalf("reset token length = %d", len(token))
	}
	if strings.Contains(mustJSON(t, known), token) {
		t.Fatal("public forgot response contained the reset token")
	}

	listed := get(t, owner, server.URL+"/api/v1/mail?page=1&pageSize=10")
	items, _ := listed["items"].([]any)
	if listed["total"] != float64(1) || len(items) != 1 {
		t.Fatalf("notification inbox included an email token or wrong total: %#v", listed)
	}
	item := items[0].(map[string]any)
	if item["subject"] != "旧通知" || strings.Contains(mustJSON(t, listed), token) {
		t.Fatalf("reset token leaked into notification history: %#v", listed)
	}

	anonymous := &http.Client{}
	deniedAnonymous := getStatus(t, anonymous, server.URL+"/api/v1/mail", http.StatusUnauthorized)
	if deniedAnonymous["error"].(map[string]any)["code"] != "unauthorized" {
		t.Fatalf("anonymous notification read = %#v", deniedAnonymous)
	}
	editor := loginAs(t, server.URL, "editor1", "password1")
	deniedEditor := getStatus(t, editor, server.URL+"/api/v1/mail", http.StatusForbidden)
	if deniedEditor["error"].(map[string]any)["code"] != "forbidden" {
		t.Fatalf("editor notification read = %#v", deniedEditor)
	}

	tooLongPassword := postJSON(t, public, server.URL+"/api/v1/auth/reset", map[string]string{"token": token, "password": strings.Repeat("r", 73)}, http.StatusUnprocessableEntity)
	if tooLongPassword["error"].(map[string]any)["code"] != "validation_error" {
		t.Fatalf("overlong reset password = %#v", tooLongPassword)
	}
	// Validation happens before consuming the single-use token.
	postJSON(t, public, server.URL+"/api/v1/auth/reset", map[string]string{"token": token, "password": "password2"}, http.StatusOK)
	signedIn := loginAs(t, server.URL, "owner", "password2")
	if signedIn == nil {
		t.Fatal("new password did not sign in")
	}
	oldPassword := postJSON(t, public, server.URL+"/api/v1/auth/login", map[string]string{"username": "owner", "password": "password1"}, http.StatusUnauthorized)
	if oldPassword["error"].(map[string]any)["code"] != "invalid_credentials" {
		t.Fatalf("old password login = %#v", oldPassword)
	}
	reused := postJSON(t, public, server.URL+"/api/v1/auth/reset", map[string]string{"token": token, "password": "password3"}, http.StatusBadRequest)
	if reused["error"].(map[string]any)["code"] != "invalid_token" {
		t.Fatalf("reused reset token = %#v", reused)
	}
}

func TestPasswordResetDeliveryFailureDoesNotRevealAccountOrLeaveToken(t *testing.T) {
	mailer := &recordingMailer{err: errors.New("SMTP unavailable")}
	repo, server, owner := newAPIServer(t, mailer)
	setupSite(t, owner, server.URL, "UTC")
	public := cookieClient(t)
	get(t, public, server.URL+"/api/v1/system/status")
	unknown := postJSON(t, public, server.URL+"/api/v1/auth/forgot", map[string]string{"username": "missing-user"}, http.StatusAccepted)
	known := postJSON(t, public, server.URL+"/api/v1/auth/forgot", map[string]string{"username": "owner"}, http.StatusAccepted)
	assertSameForgotShape(t, unknown, known)

	messages := mailer.snapshot()
	if len(messages) != 1 {
		t.Fatalf("failed SMTP attempts = %#v", messages)
	}
	token := resetToken(messages[0].body)
	if len(token) != 64 {
		t.Fatalf("reset token length = %d", len(token))
	}
	invalid := postJSON(t, public, server.URL+"/api/v1/auth/reset", map[string]string{"token": token, "password": "password2"}, http.StatusBadRequest)
	if invalid["error"].(map[string]any)["code"] != "invalid_token" {
		t.Fatalf("failed-delivery token remained usable: %#v", invalid)
	}
	if _, err := repo.ListOutboundMail(1, 20); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledForgotRequestWatchesLateDeliveryFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	delivery := make(chan error, 1)
	deliveryFailed := make(chan struct{}, 1)

	if waitForPasswordResetResponse(ctx, time.Now(), delivery, func() {
		deliveryFailed <- struct{}{}
	}) {
		t.Fatal("canceled forgot request was marked for a response")
	}
	delivery <- errors.New("late SMTP failure")
	select {
	case <-deliveryFailed:
	case <-time.After(time.Second):
		t.Fatal("late mail failure did not run its cleanup callback")
	}
}

func TestPasswordResetRequiresConfiguredMailerWithoutAccountDisclosure(t *testing.T) {
	_, server, client := newAPIServer(t)
	setupSite(t, client, server.URL, "UTC")
	get(t, client, server.URL+"/api/v1/system/status")
	firstKnownStatus, firstKnown := directForgotStatus(t, server.Config.Handler, "192.0.2.40:1234", "owner")
	repeatedKnownStatus, repeatedKnown := directForgotStatus(t, server.Config.Handler, "198.51.100.40:1234", "owner")
	unknownStatus, unknown := directForgotStatus(t, server.Config.Handler, "198.51.100.41:1234", "missing-user")
	for label, result := range map[string]struct {
		status   int
		response map[string]any
	}{
		"first known account":    {firstKnownStatus, firstKnown},
		"repeated known account": {repeatedKnownStatus, repeatedKnown},
		"unknown account":        {unknownStatus, unknown},
	} {
		if result.status != http.StatusServiceUnavailable {
			t.Errorf("%s status = %d, want %d", label, result.status, http.StatusServiceUnavailable)
		}
	}
	if mustJSON(t, firstKnown) != mustJSON(t, repeatedKnown) || mustJSON(t, firstKnown) != mustJSON(t, unknown) {
		t.Fatalf("SMTP configuration failure revealed account existence: first=%#v repeated=%#v unknown=%#v", firstKnown, repeatedKnown, unknown)
	}
}

func TestPendingCommentSurvivesSMTPFailureAndKeepsInboxNotice(t *testing.T) {
	mailer := &recordingMailer{err: errors.New("SMTP unavailable")}
	_, server, owner := newAPIServer(t, mailer)
	setupSite(t, owner, server.URL, "UTC")
	postJSON(t, owner, server.URL+"/api/v1/content/posts", map[string]any{
		"title": "待评论文章", "slug": "mail-comment", "content": "正文", "status": "published",
	}, http.StatusCreated)
	public := cookieClient(t)
	get(t, public, server.URL+"/api/v1/system/status")
	comment := postJSON(t, public, server.URL+"/api/v1/content/posts/mail-comment/comments", map[string]string{
		"authorName": "读者", "content": "即使 SMTP 失败也应保存",
	}, http.StatusAccepted)
	if comment["status"] != "pending" {
		t.Fatalf("comment = %#v", comment)
	}
	messages := mailer.snapshot()
	if len(messages) != 1 || messages[0].to != "owner@example.com" || messages[0].subject != "新评论待审核" {
		t.Fatalf("comment notification delivery = %#v", messages)
	}
	inbox := get(t, owner, server.URL+"/api/v1/mail?page=1&pageSize=20")
	if inbox["total"] != float64(1) || !strings.Contains(mustJSON(t, inbox), "即使 SMTP 失败也应保存") {
		t.Fatalf("comment notification inbox = %#v", inbox)
	}
}

func TestForgotPasswordRateLimit(t *testing.T) {
	_, server, client := newAPIServer(t)
	setupSite(t, client, server.URL, "UTC")
	get(t, client, server.URL+"/api/v1/system/status")
	for attempt := 0; attempt < 5; attempt++ {
		postJSON(t, client, server.URL+"/api/v1/auth/forgot", map[string]string{"username": "missing-user"}, http.StatusServiceUnavailable)
	}
	limited := postJSON(t, client, server.URL+"/api/v1/auth/forgot", map[string]string{"username": "missing-user"}, http.StatusTooManyRequests)
	if limited["error"].(map[string]any)["code"] != "rate_limited" {
		t.Fatalf("forgot rate limit = %#v", limited)
	}
}

func TestOutboundMailPaginationAndRetention(t *testing.T) {
	repo, server, owner := newAPIServer(t)
	setupSite(t, owner, server.URL, "UTC")
	for index := 0; index < 25; index++ {
		if err := repo.SaveOutboundMail("owner@example.com", "通知", "内容"); err != nil {
			t.Fatal(err)
		}
	}
	second := get(t, owner, server.URL+"/api/v1/mail?page=2&pageSize=10")
	items, _ := second["items"].([]any)
	if second["total"] != float64(25) || second["page"] != float64(2) || second["pageSize"] != float64(10) || len(items) != 10 {
		t.Fatalf("notification pagination = %#v", second)
	}
}

func cookieClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

func assertSameForgotShape(t *testing.T, unknown, known map[string]any) {
	t.Helper()
	if unknown["status"] != "accepted" || known["status"] != "accepted" || unknown["message"] != known["message"] {
		t.Fatalf("forgot responses differ: unknown=%#v known=%#v", unknown, known)
	}
	if unknown["message"] != "如果账户存在，重置口令已发送到该账户的邮箱" {
		t.Fatalf("forgot message = %#v", unknown["message"])
	}
	unknownKeys := mapKeys(unknown)
	knownKeys := mapKeys(known)
	if len(unknownKeys) != 2 || strings.Join(unknownKeys, ",") != strings.Join(knownKeys, ",") || strings.Join(knownKeys, ",") != "message,status" {
		t.Fatalf("forgot shape unknown=%v known=%v", unknownKeys, knownKeys)
	}
}

func mapKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}
