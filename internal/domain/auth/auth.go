package auth

import "time"

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	Disabled     bool   `json:"-"`
}
type Credentials struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
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
