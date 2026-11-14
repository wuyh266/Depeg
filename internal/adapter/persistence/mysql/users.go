package mysql

import (
	"context"
	"time"

	"depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/port"
)

type userRepository struct{ database Database }

func NewUserRepository(database Database) port.UserRepository {
	return &userRepository{database: database}
}
func (r *userRepository) Create(ctx context.Context, user auth.User) error {
	db, err := r.database.Session(ctx)
	if err != nil {
		return err
	}
	var email *string
	if user.Email != "" {
		email = &user.Email
	}
	return translate(db.Create(&userModel{
		ID: user.ID, Username: user.Username, PasswordHash: user.PasswordHash, Disabled: user.Disabled,
		Email: email, EmailVerifiedAt: user.EmailVerifiedAt,
	}).Error)
}
func (r *userRepository) FindByUsername(ctx context.Context, username string) (auth.User, error) {
	db, err := r.database.Session(ctx)
	if err != nil {
		return auth.User{}, err
	}
	var row userModel
	if err := db.Where("username = ?", username).Take(&row).Error; err != nil {
		return auth.User{}, translate(err)
	}
	return row.user(), nil
}
func (r *userRepository) FindByID(ctx context.Context, id string) (auth.User, error) {
	db, err := r.database.Session(ctx)
	if err != nil {
		return auth.User{}, err
	}
	var row userModel
	if err := db.Where("id = ?", id).Take(&row).Error; err != nil {
		return auth.User{}, translate(err)
	}
	return row.user(), nil
}

func (r *userRepository) FindByVerifiedEmail(ctx context.Context, email string) (auth.User, error) {
	db, err := r.database.Session(ctx)
	if err != nil {
		return auth.User{}, err
	}
	var row userModel
	if err := db.Where("email = ? AND email_verified_at IS NOT NULL AND disabled = ?", email, false).Take(&row).Error; err != nil {
		return auth.User{}, translate(err)
	}
	return row.user(), nil
}

func (r *userRepository) ResetPassword(ctx context.Context, userID, verifiedEmail, passwordHash string) error {
	return r.database.Transaction(ctx, func(tx Database) error {
		db, err := tx.Session(ctx)
		if err != nil {
			return err
		}
		// Updating the user locks its row until all sessions are revoked.
		updated := db.Model(&userModel{}).
			Where("id = ? AND email = ? AND email_verified_at IS NOT NULL AND disabled = ?", userID, verifiedEmail, false).
			Update("password_hash", passwordHash)
		if updated.Error != nil {
			return translate(updated.Error)
		}
		if updated.RowsAffected != 1 {
			return auth.ErrInvalidEmailCode
		}
		return translate(db.Model(&sessionModel{}).Where("user_id = ? AND revoked_at IS NULL", userID).
			Update("revoked_at", time.Now().UTC()).Error)
	})
}

func (row userModel) user() auth.User {
	user := auth.User{
		ID: row.ID, Username: row.Username, PasswordHash: row.PasswordHash,
		Disabled: row.Disabled, EmailVerifiedAt: row.EmailVerifiedAt,
	}
	if row.Email != nil {
		user.Email = *row.Email
	}
	return user
}
