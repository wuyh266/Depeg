package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/port"
)

type sender struct {
	settings  config.SMTP
	logger    port.Logger
	tlsConfig *tls.Config
}

func New(settings config.SMTP, loggers ...port.Logger) port.EmailCodeMailer {
	if settings.Address == "" {
		return nil
	}
	s := &sender{settings: settings}
	if len(loggers) > 0 {
		s.logger = loggers[0]
	}
	return s
}

func (s *sender) SendRegistrationCode(ctx context.Context, email, code string) error {
	return s.sendCode(ctx, email, code, "稳定币观察台注册验证码", "注册验证码")
}

func (s *sender) SendPasswordResetCode(ctx context.Context, email, code string) error {
	return s.sendCode(ctx, email, code, "稳定币观察台密码重置验证码", "密码重置验证码")
}

func (s *sender) sendCode(ctx context.Context, email, code, subject, purpose string) error {
	for _, address := range []string{s.settings.From, email} {
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address || strings.ContainsAny(address, "\r\n") {
			return auth.ErrEmailDeliveryFailed
		}
	}
	if err := s.send(ctx, email, codeMessage(s.settings.From, email, code, subject, purpose)); err != nil {
		stage, reason, status := failureDetails(err)
		fields := []any{"stage", stage, "reason", reason, "smtp_status", status, "purpose", purpose}
		// Do not log raw SMTP errors: they can contain addresses, credentials or codes.
		if s.logger != nil {
			s.logger.Error(ctx, "SMTP delivery failed", fields...)
		} else {
			slog.ErrorContext(ctx, "SMTP delivery failed", fields...)
		}
		return auth.ErrEmailDeliveryFailed
	}
	return nil
}

type deliveryError struct {
	stage string
	cause error
}

func (e *deliveryError) Error() string { return "SMTP " + e.stage + " failed" }
func (e *deliveryError) Unwrap() error { return e.cause }
func at(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &deliveryError{stage, err}
}

func failureDetails(err error) (stage, reason string, status int) {
	stage, reason = "unknown", "transport_error"
	var failure *deliveryError
	if errors.As(err, &failure) {
		stage = failure.stage
	}
	var reply *textproto.Error
	if errors.As(err, &reply) {
		return stage, "smtp_rejected", reply.Code
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		reason = "timeout"
	}
	if errors.Is(err, context.Canceled) {
		reason = "canceled"
	}
	var certificate *tls.CertificateVerificationError
	if errors.As(err, &certificate) {
		reason = "tls_verification_failed"
	}
	if stage == "authenticate" && reason == "transport_error" {
		reason = "authentication_unavailable"
	}
	return stage, reason, 0
}

// LOGIN is needed by providers which do not advertise AUTH PLAIN.
// Like PlainAuth, credentials must only travel over an authenticated TLS connection.
type loginAuth struct {
	username, password, host string
	step                     int
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS || server.Name != a.host {
		return "", nil, fmt.Errorf("LOGIN requires TLS and the expected server")
	}
	a.step = 0
	return "LOGIN", nil, nil
}
func (a *loginAuth) Next(_ []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	a.step++
	switch a.step {
	case 1:
		return []byte(a.username), nil
	case 2:
		return []byte(a.password), nil
	}
	return nil, fmt.Errorf("unexpected LOGIN challenge")
}

func (s *sender) send(ctx context.Context, recipient string, message []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	host, _, err := net.SplitHostPort(s.settings.Address)
	if err != nil {
		return at("configuration", err)
	}
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if s.tlsConfig != nil {
		tlsConfig = s.tlsConfig.Clone()
		tlsConfig.ServerName = host
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.settings.Address)
	if err != nil {
		return at("connect", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return at("connect", err)
	}

	var transport net.Conn = conn
	mode := s.settings.ResolvedTLSMode()
	if mode == "tls" {
		transport = tls.Client(conn, tlsConfig)
	}
	client, err := smtp.NewClient(transport, host)
	if err != nil {
		return at("greeting", err)
	}
	defer client.Close()
	if mode == "starttls" {
		if err := client.StartTLS(tlsConfig); err != nil {
			return at("starttls", err)
		}
	}
	if s.settings.Username != "" {
		_, mechanisms := client.Extension("AUTH")
		var authentication smtp.Auth
		for _, mechanism := range strings.Fields(mechanisms) {
			if strings.EqualFold(mechanism, "PLAIN") {
				authentication = smtp.PlainAuth("", s.settings.Username, s.settings.Password, host)
				break
			}
			if strings.EqualFold(mechanism, "LOGIN") {
				authentication = &loginAuth{username: s.settings.Username, password: s.settings.Password, host: host}
			}
		}
		if authentication == nil {
			return at("authenticate", fmt.Errorf("server supports neither PLAIN nor LOGIN"))
		}
		if err := client.Auth(authentication); err != nil {
			return at("authenticate", err)
		}
	}
	if err := client.Mail(s.settings.From); err != nil {
		return at("mail_from", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return at("recipient", err)
	}
	data, err := client.Data()
	if err != nil {
		return at("data", err)
	}
	if _, err := data.Write(message); err != nil {
		return at("data", err)
	}
	if err := data.Close(); err != nil {
		return at("data", err)
	}
	// DATA has already been accepted, even if closing the connection fails.
	_ = client.Quit()
	return nil
}

func codeMessage(from, to, code, subject, purpose string) []byte {
	var message bytes.Buffer
	fmt.Fprintf(&message, "From: %s\r\nTo: %s\r\nSubject: %s\r\n", from, to, mime.QEncoding.Encode("UTF-8", subject))
	fmt.Fprintf(&message, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	message.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	body := quotedprintable.NewWriter(&message)
	fmt.Fprintf(body, "你的稳定币观察台%s是 %s。\r\n验证码 %d 分钟内有效，只能使用一次。\r\n", purpose, code, int(auth.EmailCodeTTL/time.Minute))
	body.Close()
	return message.Bytes()
}
