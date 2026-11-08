package jwt_test

import (
	"context"
	adapter "depeg.local/platform/internal/adapter/security/jwt"
	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/domain"
	"errors"
	jwtlib "github.com/golang-jwt/jwt/v5"
	"strings"
	"testing"
	"time"
)

func TestJWTSignatureClaimsAndTokenPurpose(t *testing.T) {
	cfg := config.JWT{Issuer: "tests", Audience: "web", SigningKey: strings.Repeat("k", 32), AccessTTL: time.Minute, RefreshTTL: time.Hour}
	manager := adapter.New(cfg)
	pair, err := manager.Issue(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Verify(context.Background(), pair.AccessToken, "access"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Verify(context.Background(), pair.RefreshToken, "access"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("refresh accepted as access")
	}
	now := time.Now()
	base := jwtlib.MapClaims{"iss": "tests", "aud": "web", "sub": "user-1", "jti": "id", "sid": "session", "token_kind": "access", "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(time.Hour).Unix()}
	tests := []struct {
		name   string
		method jwtlib.SigningMethod
		key    string
		alter  func(jwtlib.MapClaims)
	}{
		{"wrong-key", jwtlib.SigningMethodHS256, strings.Repeat("x", 32), nil},
		{"wrong-algorithm", jwtlib.SigningMethodHS384, cfg.SigningKey, nil},
		{"expired", jwtlib.SigningMethodHS256, cfg.SigningKey, func(c jwtlib.MapClaims) { c["exp"] = now.Add(-time.Minute).Unix() }},
		{"missing-exp", jwtlib.SigningMethodHS256, cfg.SigningKey, func(c jwtlib.MapClaims) { delete(c, "exp") }},
		{"wrong-audience", jwtlib.SigningMethodHS256, cfg.SigningKey, func(c jwtlib.MapClaims) { c["aud"] = "other" }},
		{"wrong-issuer", jwtlib.SigningMethodHS256, cfg.SigningKey, func(c jwtlib.MapClaims) { c["iss"] = "other" }},
		{"not-yet-valid", jwtlib.SigningMethodHS256, cfg.SigningKey, func(c jwtlib.MapClaims) { c["nbf"] = now.Add(time.Hour).Unix() }},
		{"missing-session", jwtlib.SigningMethodHS256, cfg.SigningKey, func(c jwtlib.MapClaims) { delete(c, "sid") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			claims := jwtlib.MapClaims{}
			for key, value := range base {
				claims[key] = value
			}
			if tc.alter != nil {
				tc.alter(claims)
			}
			raw, err := jwtlib.NewWithClaims(tc.method, claims).SignedString([]byte(tc.key))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Verify(context.Background(), raw, "access"); !errors.Is(err, domain.ErrUnauthorized) {
				t.Fatalf("unsafe token accepted: %v", err)
			}
		})
	}
}
