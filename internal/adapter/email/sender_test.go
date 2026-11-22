package email

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/domain/auth"
)

type mailLog struct{ fields string }

func (*mailLog) Info(context.Context, string, ...any)               {}
func (l *mailLog) Error(_ context.Context, _ string, fields ...any) { l.fields = fmt.Sprint(fields...) }

func testTLS(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "SMTP test"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}, &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
}

// One local SMTP connection; no messages can reach a real mailbox.
func smtpFixture(t *testing.T, mode, mechanism, reject string) (config.SMTP, *tls.Config, <-chan string) {
	t.Helper()
	serverTLS, clientTLS := testTLS(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	settings := config.SMTP{Address: listener.Addr().String(), From: "sender@example.test", TLSMode: mode}
	if mechanism != "" {
		settings.Username = "sender@example.test"
		settings.Password = "test-password"
	}
	message := make(chan string, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	t.Cleanup(func() { listener.Close(); wg.Wait() })
	go func() {
		defer wg.Done()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		if mode == "tls" {
			conn = tls.Server(conn, serverTLS)
		}
		protocol := textproto.NewConn(conn)
		protocol.PrintfLine("220 SMTP test")
		for {
			line, err := protocol.ReadLine()
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				protocol.PrintfLine("250-localhost")
				if mode == "starttls" {
					protocol.PrintfLine("250-STARTTLS")
				}
				if mechanism != "" {
					protocol.PrintfLine("250-AUTH %s", mechanism)
				}
				protocol.PrintfLine("250 OK")
			case line == "STARTTLS":
				protocol.PrintfLine("220 ready")
				conn = tls.Server(conn, serverTLS)
				protocol = textproto.NewConn(conn)
			case strings.HasPrefix(line, "AUTH PLAIN"):
				payload, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN "))
				if reject == "auth" || string(payload) != "\x00"+settings.Username+"\x00"+settings.Password {
					protocol.PrintfLine("535 sensitive-response-secret")
				} else {
					protocol.PrintfLine("235 OK")
				}
			case line == "AUTH LOGIN":
				protocol.PrintfLine("334 VXNlcm5hbWU6")
				username, _ := protocol.ReadLine()
				protocol.PrintfLine("334 UGFzc3dvcmQ6")
				password, _ := protocol.ReadLine()
				if reject == "auth" || username != base64.StdEncoding.EncodeToString([]byte(settings.Username)) || password != base64.StdEncoding.EncodeToString([]byte(settings.Password)) {
					protocol.PrintfLine("535 sensitive-response-secret")
				} else {
					protocol.PrintfLine("235 OK")
				}
			case strings.HasPrefix(line, "MAIL FROM"):
				protocol.PrintfLine("250 OK")
			case strings.HasPrefix(line, "RCPT TO"):
				if reject == "recipient" {
					protocol.PrintfLine("550 sensitive-response-secret")
				} else {
					protocol.PrintfLine("250 OK")
				}
			case line == "DATA":
				protocol.PrintfLine("354 send data")
				body, err := io.ReadAll(protocol.DotReader())
				if err != nil {
					return
				}
				if reject == "data" {
					protocol.PrintfLine("554 sensitive-response-secret")
				} else {
					message <- string(body)
					protocol.PrintfLine("250 accepted")
				}
			case line == "QUIT":
				if reject == "quit" {
					return
				}
				protocol.PrintfLine("221 bye")
				return
			default:
				protocol.PrintfLine("500 unexpected command")
			}
		}
	}()
	return settings, clientTLS, message
}

func TestSMTPDeliveryTLSAndStartTLS(t *testing.T) {
	for _, mode := range []string{"tls", "starttls", "none"} {
		mechanisms := []string{"PLAIN", "LOGIN"}
		if mode == "none" {
			mechanisms = []string{""}
		}
		for _, mechanism := range mechanisms {
			t.Run(mode+mechanism, func(t *testing.T) {
				settings, trust, received := smtpFixture(t, mode, mechanism, "")
				s := &sender{settings: settings, tlsConfig: trust}
				if err := s.SendRegistrationCode(context.Background(), "recipient@example.test", "123456"); err != nil {
					t.Fatal(err)
				}
				select {
				case raw := <-received:
					parsed, err := mail.ReadMessage(strings.NewReader(raw))
					if err != nil {
						t.Fatal(err)
					}
					subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
					if err != nil || subject != "稳定币观察台注册验证码" {
						t.Fatal("invalid subject")
					}
					body, err := io.ReadAll(quotedprintable.NewReader(parsed.Body))
					if err != nil || !strings.Contains(string(body), "123456") {
						t.Fatal("invalid code body")
					}
				default:
					t.Fatal("message not delivered")
				}
			})
		}
	}
}

func TestSMTPFailuresAreLoggedWithoutSecrets(t *testing.T) {
	for _, failure := range []string{"auth", "recipient", "data"} {
		t.Run(failure, func(t *testing.T) {
			settings, trust, _ := smtpFixture(t, "tls", "LOGIN", failure)
			log := &mailLog{}
			s := &sender{settings: settings, tlsConfig: trust, logger: log}
			err := s.SendPasswordResetCode(context.Background(), "recipient@example.test", "123456")
			if !errors.Is(err, auth.ErrEmailDeliveryFailed) {
				t.Fatalf("expected delivery failure: %v", err)
			}
			for _, secret := range []string{"sensitive-response-secret", "123456", "recipient@example.test", settings.Password} {
				if strings.Contains(log.fields, secret) || strings.Contains(err.Error(), secret) {
					t.Fatal("delivery error exposed private data")
				}
			}
			if !strings.Contains(log.fields, "smtp_rejected") || !strings.Contains(log.fields, "stage") {
				t.Fatal("missing diagnostic classification")
			}
		})
	}
}

func TestSMTPContextCancelsBlockedGreeting(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(io.Discard, conn)
	}()
	s := &sender{settings: config.SMTP{Address: listener.Addr().String(), TLSMode: "none", From: "sender@example.test"}, logger: &mailLog{}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = s.SendRegistrationCode(ctx, "recipient@example.test", "123456")
	if !errors.Is(err, auth.ErrEmailDeliveryFailed) || time.Since(start) > time.Second {
		t.Fatal("blocked SMTP ignored context")
	}
	listener.Close()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("SMTP fixture did not stop")
	}
}

func TestLOGINRejectsPlaintextAndWrongHost(t *testing.T) {
	a := &loginAuth{host: "smtp.example.test"}
	for _, server := range []*smtp.ServerInfo{{Name: a.host}, {Name: "other", TLS: true}} {
		if _, _, err := a.Start(server); err == nil {
			t.Fatal("unsafe LOGIN accepted")
		}
	}
}

func TestSMTPRejectsUntrustedCertificate(t *testing.T) {
	settings, _, received := smtpFixture(t, "tls", "PLAIN", "")
	log := &mailLog{}
	s := &sender{settings: settings, logger: log}
	if err := s.SendRegistrationCode(context.Background(), "recipient@example.test", "123456"); !errors.Is(err, auth.ErrEmailDeliveryFailed) {
		t.Fatal("untrusted certificate accepted")
	}
	if !strings.Contains(log.fields, "tls_verification_failed") {
		t.Fatal("missing TLS diagnostic")
	}
	select {
	case <-received:
		t.Fatal("mail sent with an untrusted certificate")
	default:
	}
}

func TestSMTPAcceptedDataRemainsSuccessfulIfQuitFails(t *testing.T) {
	settings, trust, received := smtpFixture(t, "tls", "PLAIN", "quit")
	s := &sender{settings: settings, tlsConfig: trust}
	if err := s.SendRegistrationCode(context.Background(), "recipient@example.test", "123456"); err != nil {
		t.Fatal("accepted message incorrectly marked failed")
	}
	select {
	case <-received:
	default:
		t.Fatal("missing accepted message")
	}
}
