package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	ModeStartTLS = "starttls"
	ModeImplicit = "implicit"
)

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	Mode     string
	Timeout  time.Duration
}

type SMTP struct {
	config            SMTPConfig
	tlsConfigOverride *tls.Config
}

// FromEnv returns nil when SMTP is not configured. A partially configured
// transport is an error so password recovery cannot silently appear enabled.
func FromEnv() (*SMTP, error) {
	keys := []string{"APP_SMTP_HOST", "APP_SMTP_USERNAME", "APP_SMTP_PASSWORD", "APP_SMTP_FROM"}
	configured := false
	for _, key := range keys {
		if os.Getenv(key) != "" {
			configured = true
			break
		}
	}
	if !configured {
		return nil, nil
	}

	mode := strings.ToLower(strings.TrimSpace(envOr("APP_SMTP_TLS_MODE", ModeStartTLS)))
	defaultPort := 587
	if mode == ModeImplicit {
		defaultPort = 465
	}
	port := defaultPort
	if raw := strings.TrimSpace(os.Getenv("APP_SMTP_PORT")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("APP_SMTP_PORT must be an integer: %w", err)
		}
		port = parsed
	}
	return New(SMTPConfig{
		Host:     strings.TrimSpace(os.Getenv("APP_SMTP_HOST")),
		Port:     port,
		Username: os.Getenv("APP_SMTP_USERNAME"),
		Password: os.Getenv("APP_SMTP_PASSWORD"),
		From:     strings.TrimSpace(os.Getenv("APP_SMTP_FROM")),
		Mode:     mode,
		Timeout:  10 * time.Second,
	})
}

func New(config SMTPConfig) (*SMTP, error) {
	config.Host = strings.TrimSpace(config.Host)
	config.From = strings.TrimSpace(config.From)
	config.Mode = strings.ToLower(strings.TrimSpace(config.Mode))
	if config.Mode == "" {
		config.Mode = ModeStartTLS
	}
	if config.Port == 0 {
		config.Port = 587
		if config.Mode == ModeImplicit {
			config.Port = 465
		}
	}
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Second
	}
	if config.Host == "" || strings.ContainsAny(config.Host, " \r\n/\\") || (strings.Contains(config.Host, ":") && net.ParseIP(config.Host) == nil) {
		return nil, fmt.Errorf("SMTP host is required and must be a hostname")
	}
	if config.Port < 1 || config.Port > 65535 {
		return nil, fmt.Errorf("SMTP port must be between 1 and 65535")
	}
	if config.Mode != ModeStartTLS && config.Mode != ModeImplicit {
		return nil, fmt.Errorf("SMTP TLS mode must be %q or %q", ModeStartTLS, ModeImplicit)
	}
	if _, err := parseAddress(config.From); err != nil {
		return nil, fmt.Errorf("SMTP sender address is invalid: %w", err)
	}
	if (config.Username == "") != (config.Password == "") {
		return nil, fmt.Errorf("SMTP username and password must either both be set or both be empty")
	}
	return &SMTP{config: config}, nil
}

func (s *SMTP) Send(ctx context.Context, recipient, subject, body string) error {
	if s == nil {
		return fmt.Errorf("SMTP is not configured")
	}
	from, err := parseAddress(s.config.From)
	if err != nil {
		return err
	}
	to, err := parseAddress(recipient)
	if err != nil {
		return fmt.Errorf("recipient address is invalid: %w", err)
	}
	if strings.ContainsAny(subject, "\r\n") {
		return fmt.Errorf("mail subject contains a line break")
	}

	address := net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))
	dialer := &net.Dialer{Timeout: s.config.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	rawConn := conn
	stopClose := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	defer stopClose()
	defer conn.Close()
	deadline := time.Now().Add(s.config.Timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set SMTP deadline: %w", err)
	}
	if s.config.Mode == ModeImplicit {
		tlsConn := tls.Client(conn, s.tlsConfig())
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("establish SMTP TLS connection: %w", err)
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, s.config.Host)
	if err != nil {
		return fmt.Errorf("start SMTP session: %w", err)
	}
	defer client.Close()
	if s.config.Mode == ModeStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP server does not support required STARTTLS")
		}
		if err := client.StartTLS(s.tlsConfig()); err != nil {
			return fmt.Errorf("establish SMTP STARTTLS session: %w", err)
		}
	}
	if s.config.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)); err != nil {
			return fmt.Errorf("authenticate with SMTP server: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	data, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP message: %w", err)
	}
	if _, err := data.Write(buildMessage(from, to, subject, body)); err != nil {
		_ = data.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := data.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("finish SMTP session: %w", err)
	}
	return nil
}

func (s *SMTP) tlsConfig() *tls.Config {
	if s.tlsConfigOverride != nil {
		return s.tlsConfigOverride.Clone()
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: s.config.Host}
}

func parseAddress(raw string) (string, error) {
	if strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\r\n") {
		return "", fmt.Errorf("invalid email address")
	}
	parsed, err := mail.ParseAddress(raw)
	if err != nil || parsed.Address == "" || strings.ContainsAny(parsed.Address, "\r\n<> ") {
		return "", fmt.Errorf("invalid email address")
	}
	return parsed.Address, nil
}

func buildMessage(from, to, subject, body string) []byte {
	var encoded bytes.Buffer
	writer := quotedprintable.NewWriter(&encoded)
	normalizedBody := strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	_, _ = io.WriteString(writer, normalizedBody)
	_ = writer.Close()
	var message bytes.Buffer
	fmt.Fprintf(&message, "From: <%s>\r\n", from)
	fmt.Fprintf(&message, "To: <%s>\r\n", to)
	fmt.Fprintf(&message, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", subject))
	fmt.Fprintf(&message, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	message.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	message.Write(encoded.Bytes())
	message.WriteString("\r\n")
	return message.Bytes()
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
