package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"depeg.local/platform/internal/domain"
	account "depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/identity"
	"depeg.local/platform/internal/port"
)

var (
	emailDomainPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
	emailCodePattern   = regexp.MustCompile(`^[0-9]{6}$`)
)

func normalizeEmail(raw string) (string, error) {
	email := strings.TrimSpace(raw)
	local, host, ok := strings.Cut(email, "@")
	if !ok || len(email) > 254 || len(local) == 0 || len(local) > 64 {
		return "", domain.ErrInvalid
	}
	host = strings.ToLower(host)
	if !emailDomainPattern.MatchString(host) || strings.IndexFunc(local, func(r rune) bool {
		return r < '!' || r > '~' || r == '"'
	}) != -1 {
		return "", domain.ErrInvalid
	}
	normalized := local + "@" + host
	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Address != normalized {
		return "", domain.ErrInvalid
	}
	return normalized, nil
}

func keyedDigest(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

func (s *service) emailCodeHash(email, code string) string {
	return hex.EncodeToString(keyedDigest(s.codeKey, "code\x00"+email+"\x00"+code))
}

func (s *service) emailSourceHash(source string) string {
	return hex.EncodeToString(keyedDigest(s.codeKey, "source\x00"+source))
}

func newEmailCode() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

func (s *service) SendRegistrationCode(ctx context.Context, rawEmail, source string) error {
	email, err := normalizeEmail(rawEmail)
	if err != nil {
		return err
	}
	if s.mailer == nil {
		return domain.ErrUnavailable
	}
	code, err := newEmailCode()
	if err != nil {
		return err
	}
	if err := s.registrationCodes.Reserve(ctx, email, s.emailCodeHash(email, code), s.emailSourceHash(source)); err != nil {
		return err
	}
	return s.mailer.SendRegistrationCode(ctx, email, code)
}

func (s *service) Register(ctx context.Context, input account.Registration) (account.User, error) {
	username := strings.ToLower(strings.TrimSpace(input.Username))
	if !usernamePattern.MatchString(username) || len(input.Password) < 8 || len(input.Password) > 72 {
		return account.User{}, domain.ErrInvalid
	}
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return account.User{}, err
	}
	if !emailCodePattern.MatchString(input.EmailCode) {
		return account.User{}, account.ErrInvalidEmailCode
	}
	if _, err := s.users.FindByUsername(ctx, username); err == nil {
		return account.User{}, domain.ErrConflict
	} else if !errors.Is(err, domain.ErrNotFound) {
		return account.User{}, fmt.Errorf("%w: user lookup", domain.ErrUnavailable)
	}

	var user account.User
	err = s.registrationCodes.Use(ctx, email, s.emailCodeHash(email, input.EmailCode), func(users port.UserRepository) error {
		hash, err := s.passwords.Hash(ctx, input.Password)
		if err != nil {
			return err
		}
		id, err := identity.New()
		if err != nil {
			return err
		}
		verifiedAt := time.Now().UTC()
		user = account.User{ID: id, Username: username, PasswordHash: hash, Email: email, EmailVerifiedAt: &verifiedAt}
		return users.Create(ctx, user)
	})
	if err != nil {
		return account.User{}, err
	}
	return user, nil
}
