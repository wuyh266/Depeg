package port

import (
	"context"
	"depeg.local/platform/internal/domain/auth"
)

type UserRepository interface {
	Create(context.Context, auth.User) error
	FindByUsername(context.Context, string) (auth.User, error)
	FindByID(context.Context, string) (auth.User, error)
	FindByVerifiedEmail(context.Context, string) (auth.User, error)
	ResetPassword(ctx context.Context, userID, verifiedEmail, passwordHash string) error
}
type PasswordHasher interface {
	Hash(context.Context, string) (string, error)
	Compare(context.Context, string, string) error
}
type TokenManager interface {
	Issue(context.Context, string) (auth.TokenPair, error)
	Verify(context.Context, string, string) (auth.Claims, error)
}
type SessionStore interface {
	Save(ctx context.Context, session auth.Session, verifiedPasswordHash string) error
	Find(context.Context, string) (auth.Session, error)
	Rotate(context.Context, string, auth.Session) error
	Revoked(context.Context, string) (bool, error)
	Revoke(context.Context, string) error
}

type EmailCodeStore interface {
	Reserve(ctx context.Context, email, codeHash, sourceHash string) error
	Use(ctx context.Context, email, codeHash string, apply func(UserRepository) error) error
}

type EmailCodeMailer interface {
	SendRegistrationCode(ctx context.Context, email, code string) error
	SendPasswordResetCode(ctx context.Context, email, code string) error
}

type AuthService interface {
	SendRegistrationCode(ctx context.Context, email, source string) error
	Register(context.Context, auth.Registration) (auth.User, error)
	RequestPasswordReset(ctx context.Context, email, source string) error
	ResetPassword(context.Context, auth.PasswordReset) error
	Login(context.Context, auth.Credentials) (auth.TokenPair, error)
	Refresh(context.Context, string) (auth.TokenPair, error)
	Authenticate(context.Context, string) (auth.User, error)
	Logout(context.Context, string) error
}
