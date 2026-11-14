package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"time"

	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/port"
)

type sender struct{ settings config.SMTP }

func New(settings config.SMTP) port.EmailCodeMailer {
	if settings.Address == "" {
		return nil
	}
	return &sender{settings: settings}
}

func (s *sender) SendRegistrationCode(ctx context.Context, email, code string) error {
	return s.sendCode(ctx, email, code, "稳定币观察台注册验证码", "注册验证码")
}

func (s *sender) SendPasswordResetCode(ctx context.Context, email, code string) error {
	return s.sendCode(ctx, email, code, "稳定币观察台密码重置验证码", "密码重置验证码")
}

func (s *sender) sendCode(ctx context.Context, email, code, subject, purpose string) error {
	if err := s.send(ctx, email, codeMessage(s.settings.From, email, code, subject, purpose)); err != nil {
		// SMTP responses can contain recipient addresses and message content.
		return domain.ErrUnavailable
	}
	return nil
}

func (s *sender) send(ctx context.Context, recipient string, message []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	host, _, err := net.SplitHostPort(s.settings.Address)
	if err != nil {
		return err
	}
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.settings.Address)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}

	var transport net.Conn = conn
	if s.settings.TLSMode == "tls" {
		transport = tls.Client(conn, tlsConfig)
	}
	client, err := smtp.NewClient(transport, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if s.settings.TLSMode == "starttls" {
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if s.settings.Username != "" {
		auth := smtp.PlainAuth("", s.settings.Username, s.settings.Password, host)
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(s.settings.From); err != nil {
		return err
	}
	if err := client.Rcpt(recipient); err != nil {
		return err
	}
	data, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := data.Write(message); err != nil {
		return err
	}
	if err := data.Close(); err != nil {
		return err
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
