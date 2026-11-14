package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"depeg.local/platform/internal/domain"
	account "depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/identity"
	"depeg.local/platform/internal/port"
)

type service struct {
	users             port.UserRepository
	passwords         port.PasswordHasher
	tokens            port.TokenManager
	sessions          port.SessionStore
	registrationCodes port.EmailCodeStore
	resetCodes        port.EmailCodeStore
	mailer            port.EmailCodeMailer
	codeKey           []byte
	resetMailSlots    chan struct{}
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{2,31}$`)

func New(users port.UserRepository, passwords port.PasswordHasher, tokens port.TokenManager, sessions port.SessionStore, registrationCodes, resetCodes port.EmailCodeStore, mailer port.EmailCodeMailer, signingKey string) port.AuthService {
	return &service{
		users: users, passwords: passwords, tokens: tokens, sessions: sessions,
		registrationCodes: registrationCodes, resetCodes: resetCodes, mailer: mailer,
		codeKey: keyedDigest([]byte(signingKey), "depeg-registration-v1"), resetMailSlots: make(chan struct{}, 16),
	}
}
func (s *service) Login(ctx context.Context, input account.Credentials) (account.TokenPair, error) {
	username := strings.ToLower(strings.TrimSpace(input.Username))
	if !usernamePattern.MatchString(username) || len(input.Password) < 8 || len(input.Password) > 72 {
		return account.TokenPair{}, domain.ErrUnauthorized
	}
	user, err := s.users.FindByUsername(ctx, username)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && user.Disabled) {
		return account.TokenPair{}, domain.ErrUnauthorized
	}
	if err != nil {
		return account.TokenPair{}, fmt.Errorf("%w: user lookup", domain.ErrUnavailable)
	}
	if err := s.passwords.Compare(ctx, user.PasswordHash, input.Password); err != nil {
		return account.TokenPair{}, domain.ErrUnauthorized
	}
	pair, err := s.tokens.Issue(ctx, user.ID)
	if err != nil {
		return account.TokenPair{}, err
	}
	session := account.Session{ID: pair.SessionID, UserID: user.ID, RefreshHash: identity.Digest(pair.RefreshToken), ExpiresAt: pair.RefreshExpiresAt}
	if err := s.sessions.Save(ctx, session, user.PasswordHash); err != nil {
		if errors.Is(err, domain.ErrUnauthorized) {
			return account.TokenPair{}, err
		}
		return account.TokenPair{}, fmt.Errorf("%w: persist session", domain.ErrUnavailable)
	}
	return pair, nil
}
func (s *service) Refresh(ctx context.Context, raw string) (account.TokenPair, error) {
	claims, err := s.tokens.Verify(ctx, raw, "refresh")
	if err != nil {
		return account.TokenPair{}, err
	}
	if _, err := s.activeUser(ctx, claims); err != nil {
		return account.TokenPair{}, err
	}
	pair, err := s.tokens.Issue(ctx, claims.Subject)
	if err != nil {
		return account.TokenPair{}, err
	}
	session := account.Session{ID: pair.SessionID, UserID: claims.Subject, RefreshHash: identity.Digest(pair.RefreshToken), ExpiresAt: pair.RefreshExpiresAt}
	if err := s.sessions.Rotate(ctx, identity.Digest(raw), session); err != nil {
		return account.TokenPair{}, err
	}
	return pair, nil
}
func (s *service) activeUser(ctx context.Context, claims account.Claims) (account.User, error) {
	session, err := s.sessions.Find(ctx, claims.SessionID)
	if errors.Is(err, domain.ErrNotFound) {
		return account.User{}, domain.ErrUnauthorized
	}
	if err != nil {
		return account.User{}, fmt.Errorf("%w: session lookup", domain.ErrUnavailable)
	}
	if session.UserID != claims.Subject || session.RevokedAt != nil || !session.ExpiresAt.After(time.Now()) {
		return account.User{}, domain.ErrUnauthorized
	}
	user, err := s.users.FindByID(ctx, claims.Subject)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && user.Disabled) {
		return account.User{}, domain.ErrUnauthorized
	}
	if err != nil {
		return account.User{}, fmt.Errorf("%w: user lookup", domain.ErrUnavailable)
	}
	return user, nil
}
func (s *service) Authenticate(ctx context.Context, raw string) (account.User, error) {
	claims, err := s.tokens.Verify(ctx, raw, "access")
	if err != nil {
		return account.User{}, err
	}
	return s.activeUser(ctx, claims)
}
func (s *service) Logout(ctx context.Context, raw string) error {
	claims, err := s.tokens.Verify(ctx, raw, "access")
	if err != nil {
		return err
	}
	return s.sessions.Revoke(ctx, claims.SessionID)
}
