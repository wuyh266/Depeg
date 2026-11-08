package password

import (
	"context"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/port"
	"golang.org/x/crypto/bcrypt"
)

type hasher struct{}

func New() port.PasswordHasher { return &hasher{} }
func (*hasher) Hash(ctx context.Context, password string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(password) < 8 || len(password) > 72 {
		return "", domain.ErrInvalid
	}
	value, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(value), err
}
func (*hasher) Compare(ctx context.Context, hash, password string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(password) > 72 || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return domain.ErrUnauthorized
	}
	return nil
}
