package port

import (
	"context"
	"depeg.local/platform/internal/domain/auth"
)

type UserRepository interface {
	Create(context.Context, auth.User) error
	FindByUsername(context.Context, string) (auth.User, error)
	FindByID(context.Context, string) (auth.User, error)
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
	Save(context.Context, auth.Session) error
	Find(context.Context, string) (auth.Session, error)
	Rotate(context.Context, string, auth.Session) error
	Revoked(context.Context, string) (bool, error)
	Revoke(context.Context, string) error
}
type AuthService interface {
	Register(context.Context, auth.Credentials) (auth.User, error)
	Login(context.Context, auth.Credentials) (auth.TokenPair, error)
	Refresh(context.Context, string) (auth.TokenPair, error)
	Authenticate(context.Context, string) (auth.User, error)
	Logout(context.Context, string) error
}
