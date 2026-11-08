package jwt

import (
	"context"
	"fmt"
	"time"

	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/identity"
	"depeg.local/platform/internal/port"
	jwtlib "github.com/golang-jwt/jwt/v5"
)

type manager struct{ settings config.JWT }
type tokenClaims struct {
	jwtlib.RegisteredClaims
	Kind      string `json:"token_kind"`
	SessionID string `json:"sid"`
}

func New(settings config.JWT) port.TokenManager { return &manager{settings: settings} }
func (m *manager) configured() bool {
	return len(m.settings.SigningKey) >= 32 && m.settings.Issuer != "" && m.settings.Audience != "" && m.settings.AccessTTL > 0 && m.settings.RefreshTTL > m.settings.AccessTTL
}
func (m *manager) Issue(ctx context.Context, subject string) (auth.TokenPair, error) {
	if err := ctx.Err(); err != nil {
		return auth.TokenPair{}, err
	}
	if !m.configured() || subject == "" {
		return auth.TokenPair{}, domain.ErrUnavailable
	}
	sessionID, err := identity.New()
	if err != nil {
		return auth.TokenPair{}, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	pair := auth.TokenPair{SessionID: sessionID, TokenType: "Bearer", AccessExpiresAt: now.Add(m.settings.AccessTTL), RefreshExpiresAt: now.Add(m.settings.RefreshTTL)}
	sign := func(kind string, expires time.Time) (string, error) {
		id, err := identity.New()
		if err != nil {
			return "", err
		}
		claims := tokenClaims{RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer: m.settings.Issuer, Subject: subject, Audience: jwtlib.ClaimStrings{m.settings.Audience},
			ID: id, IssuedAt: jwtlib.NewNumericDate(now), NotBefore: jwtlib.NewNumericDate(now), ExpiresAt: jwtlib.NewNumericDate(expires),
		}, Kind: kind, SessionID: sessionID}
		return jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).SignedString([]byte(m.settings.SigningKey))
	}
	pair.AccessToken, err = sign("access", pair.AccessExpiresAt)
	if err != nil {
		return auth.TokenPair{}, err
	}
	pair.RefreshToken, err = sign("refresh", pair.RefreshExpiresAt)
	if err != nil {
		return auth.TokenPair{}, err
	}
	return pair, nil
}
func (m *manager) Verify(ctx context.Context, raw, kind string) (auth.Claims, error) {
	if err := ctx.Err(); err != nil {
		return auth.Claims{}, err
	}
	if !m.configured() {
		return auth.Claims{}, domain.ErrUnavailable
	}
	if len(raw) == 0 || len(raw) > 8192 || (kind != "access" && kind != "refresh") {
		return auth.Claims{}, domain.ErrUnauthorized
	}
	claims := &tokenClaims{}
	token, err := jwtlib.ParseWithClaims(raw, claims, func(token *jwtlib.Token) (any, error) {
		if token.Method != jwtlib.SigningMethodHS256 {
			return nil, fmt.Errorf("invalid signing algorithm")
		}
		return []byte(m.settings.SigningKey), nil
	}, jwtlib.WithValidMethods([]string{"HS256"}), jwtlib.WithIssuer(m.settings.Issuer), jwtlib.WithAudience(m.settings.Audience), jwtlib.WithExpirationRequired(), jwtlib.WithIssuedAt(), jwtlib.WithLeeway(5*time.Second))
	if err != nil || token == nil || !token.Valid || claims.Kind != kind || claims.Subject == "" || claims.ID == "" || claims.SessionID == "" || claims.IssuedAt == nil || claims.NotBefore == nil || claims.ExpiresAt == nil {
		return auth.Claims{}, domain.ErrUnauthorized
	}
	return auth.Claims{Subject: claims.Subject, TokenID: claims.ID, SessionID: claims.SessionID, Kind: claims.Kind, Issuer: claims.Issuer, Audience: []string(claims.Audience), IssuedAt: claims.IssuedAt.Time, NotBefore: claims.NotBefore.Time, ExpiresAt: claims.ExpiresAt.Time}, nil
}
