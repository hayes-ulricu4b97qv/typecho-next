package mailer

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

type capturedSMTPMessage struct {
	from string
	to   string
	body string
}

func TestImplicitTLSSMTPDeliversEncodedMessage(t *testing.T) {
	certificate, cert := testCertificate(t)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan capturedSMTPMessage, 1)
	serverErrors := make(chan error, 1)
	go serveOneSMTPMessage(listener, received, serverErrors)

	pool := x509.NewCertPool()
	pool.AddCert(cert)
	address := listener.Addr().(*net.TCPAddr)
	mailer, err := New(SMTPConfig{
		Host: "127.0.0.1", Port: address.Port, From: "blog@example.com", Mode: ModeImplicit,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	mailer.tlsConfigOverride = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "127.0.0.1", RootCAs: pool}
	if err := mailer.Send(context.Background(), "owner@example.com", "重置密码", "口令：abc123\n第二行"); err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-received:
		if message.from != "blog@example.com" || message.to != "owner@example.com" {
			t.Fatalf("SMTP envelope from=%q to=%q", message.from, message.to)
		}
		if !strings.Contains(message.body, "Subject: =?UTF-8?q?") || !strings.Contains(message.body, "Content-Transfer-Encoding: quoted-printable") || !strings.Contains(message.body, "abc123") {
			t.Fatalf("SMTP message headers/body are malformed: %q", message.body)
		}
	case err := <-serverErrors:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("SMTP test server did not receive a message")
	}
}

func TestStartTLSIsRequired(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		if _, err := fmt.Fprint(conn, "220 test SMTP\r\n"); err != nil {
			serverDone <- err
			return
		}
		if _, err := reader.ReadString('\n'); err != nil {
			serverDone <- err
			return
		}
		_, err = fmt.Fprint(conn, "250 test SMTP\r\n")
		serverDone <- err
	}()
	address := listener.Addr().(*net.TCPAddr)
	mailer, err := New(SMTPConfig{Host: "127.0.0.1", Port: address.Port, From: "blog@example.com", Mode: ModeStartTLS, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	err = mailer.Send(context.Background(), "owner@example.com", "notification", "body")
	if err == nil || !strings.Contains(err.Error(), "required STARTTLS") {
		t.Fatalf("unencrypted SMTP delivery error = %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestSMTPConfigurationRejectsInvalidAddressesAndCredentials(t *testing.T) {
	for _, config := range []SMTPConfig{
		{Host: "smtp.example.com", From: "invalid", Mode: ModeStartTLS},
		{Host: "smtp.example.com", From: "blog@example.com", Username: "user", Mode: ModeStartTLS},
		{Host: "smtp.example.com", From: "blog@example.com", Port: 65536, Mode: ModeStartTLS},
	} {
		if _, err := New(config); err == nil {
			t.Fatalf("accepted invalid SMTP config: %#v", config)
		}
	}
	mailer, err := New(SMTPConfig{Host: "smtp.example.com", From: "blog@example.com", Mode: ModeStartTLS})
	if err != nil {
		t.Fatal(err)
	}
	if err := mailer.Send(context.Background(), "victim@example.com\r\nBcc: attacker@example.com", "notice", "body"); err == nil {
		t.Fatal("accepted a recipient with a header injection")
	}
}

func TestFromEnvKeepsSMTPDisabledForBlankTemplateAndRejectsPartialConfig(t *testing.T) {
	for key, value := range map[string]string{
		"APP_SMTP_HOST": "", "APP_SMTP_PORT": "587", "APP_SMTP_USERNAME": "",
		"APP_SMTP_PASSWORD": "", "APP_SMTP_FROM": "", "APP_SMTP_TLS_MODE": ModeStartTLS,
	} {
		t.Setenv(key, value)
	}
	configured, err := FromEnv()
	if err != nil || configured != nil {
		t.Fatalf("blank SMTP template = %#v, %v", configured, err)
	}
	t.Setenv("APP_SMTP_HOST", "smtp.example.com")
	if configured, err := FromEnv(); err == nil || configured != nil {
		t.Fatalf("partial SMTP config = %#v, %v", configured, err)
	}
}

func serveOneSMTPMessage(listener net.Listener, received chan<- capturedSMTPMessage, serverErrors chan<- error) {
	conn, err := listener.Accept()
	if err != nil {
		serverErrors <- err
		return
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	if _, err := fmt.Fprint(conn, "220 test SMTP\r\n"); err != nil {
		serverErrors <- err
		return
	}
	message := capturedSMTPMessage{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			serverErrors <- err
			return
		}
		command := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(strings.ToUpper(command), "EHLO "):
			_, err = fmt.Fprint(conn, "250 test SMTP\r\n")
		case strings.HasPrefix(strings.ToUpper(command), "MAIL FROM:"):
			message.from = trimSMTPPath(command[len("MAIL FROM:"):])
			_, err = fmt.Fprint(conn, "250 sender accepted\r\n")
		case strings.HasPrefix(strings.ToUpper(command), "RCPT TO:"):
			message.to = trimSMTPPath(command[len("RCPT TO:"):])
			_, err = fmt.Fprint(conn, "250 recipient accepted\r\n")
		case strings.EqualFold(command, "DATA"):
			_, err = fmt.Fprint(conn, "354 send message\r\n")
			if err == nil {
				for {
					var bodyLine string
					bodyLine, err = reader.ReadString('\n')
					if err != nil || bodyLine == ".\r\n" {
						break
					}
					if strings.HasPrefix(bodyLine, "..") {
						bodyLine = bodyLine[1:]
					}
					message.body += bodyLine
				}
			}
			if err == nil {
				received <- message
				_, err = fmt.Fprint(conn, "250 queued\r\n")
			}
		case strings.EqualFold(command, "QUIT"):
			_, err = fmt.Fprint(conn, "221 bye\r\n")
			if err != nil {
				serverErrors <- err
			}
			return
		default:
			_, err = fmt.Fprint(conn, "500 unsupported command\r\n")
		}
		if err != nil {
			serverErrors <- err
			return
		}
	}
}

func trimSMTPPath(value string) string {
	value = strings.TrimSpace(value)
	return strings.Trim(value, "<>")
}

func testCertificate(t *testing.T) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey}),
	)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, parsed
}
