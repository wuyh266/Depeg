package auth

import (
	"depeg.local/platform/internal/domain"
	"errors"
	"time"
)

const (
	EmailCodeTTL         = 10 * time.Minute
	EmailCodeCooldown    = time.Minute
	EmailCodeMaxAttempts = 5
	EmailSendWindow      = 10 * time.Minute
	EmailSendLimit       = 10
)

var (
	ErrEmailNotConfigured  = errors.Join(domain.ErrUnavailable, errors.New("email is not configured"))
	ErrEmailDeliveryFailed = errors.Join(domain.ErrUnavailable, errors.New("email delivery failed"))
	ErrInvalidEmailCode    = errors.New("invalid or expired email code")
	ErrEmailCodeCooldown   = errors.New("email code was sent recently")
	ErrEmailSendLimit      = errors.New("email send limit reached")
)

type User struct {
	ID              string     `json:"id"`
	Username        string     `json:"username"`
	Email           string     `json:"-"`
	EmailVerifiedAt *time.Time `json:"-"`
	PasswordHash    string     `json:"-"`
	Disabled        bool       `json:"-"`
}
type Credentials struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type Registration struct {
	Credentials
	Email     string `json:"email" binding:"required"`
	EmailCode string `json:"email_code" binding:"required"`
}

type PasswordReset struct {
	Email     string `json:"email" binding:"required"`
	EmailCode string `json:"email_code" binding:"required"`
	Password  string `json:"password" binding:"required"`
}

type Claims struct {
	Subject   string
	TokenID   string
	SessionID string
	Kind      string
	Issuer    string
	Audience  []string
	IssuedAt  time.Time
	NotBefore time.Time
	ExpiresAt time.Time
}
type TokenPair struct {
	SessionID        string    `json:"-"`
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	TokenType        string    `json:"token_type"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}
type Session struct {
	ID          string
	UserID      string
	RefreshHash string
	ExpiresAt   time.Time
	RevokedAt   *time.Time
}
