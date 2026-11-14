package mysql

import (
	"context"
	"errors"
	"time"

	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/port"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type sessions struct{ database Database }

func NewSessionStore(database Database) port.SessionStore { return &sessions{database: database} }
func (s *sessions) Save(ctx context.Context, session auth.Session, verifiedPasswordHash string) error {
	return s.database.Transaction(ctx, func(tx Database) error {
		db, err := tx.Session(ctx)
		if err != nil {
			return err
		}
		user, err := lockSessionUser(db, session.UserID)
		if err != nil {
			return err
		}
		// The password can change between bcrypt verification and issuing a session.
		if user.PasswordHash != verifiedPasswordHash {
			return domain.ErrUnauthorized
		}
		return insertSession(db, session)
	})
}

func lockSessionUser(db *gorm.DB, id string) (userModel, error) {
	var user userModel
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "password_hash", "disabled").Where("id = ?", id).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && user.Disabled) {
		return userModel{}, domain.ErrUnauthorized
	}
	return user, translate(err)
}

func insertSession(db *gorm.DB, session auth.Session) error {
	return translate(db.Create(&sessionModel{ID: session.ID, UserID: session.UserID, RefreshHash: session.RefreshHash, ExpiresAt: session.ExpiresAt}).Error)
}
func (s *sessions) Find(ctx context.Context, id string) (auth.Session, error) {
	db, err := s.database.Session(ctx)
	if err != nil {
		return auth.Session{}, err
	}
	var row sessionModel
	if err := db.Where("id = ?", id).Take(&row).Error; err != nil {
		return auth.Session{}, translate(err)
	}
	return auth.Session{ID: row.ID, UserID: row.UserID, RefreshHash: row.RefreshHash, ExpiresAt: row.ExpiresAt, RevokedAt: row.RevokedAt}, nil
}
func (s *sessions) Rotate(ctx context.Context, oldHash string, next auth.Session) error {
	return s.database.Transaction(ctx, func(database Database) error {
		db, err := database.Session(ctx)
		if err != nil {
			return err
		}
		if _, err := lockSessionUser(db, next.UserID); err != nil {
			return err
		}
		now := time.Now().UTC()
		result := db.Model(&sessionModel{}).Where("refresh_hash = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > ?", oldHash, next.UserID, now).Update("revoked_at", now)
		if result.Error != nil {
			return translate(result.Error)
		}
		if result.RowsAffected != 1 {
			return domain.ErrUnauthorized
		}
		return insertSession(db, next)
	})
}
func (s *sessions) Revoked(ctx context.Context, id string) (bool, error) {
	row, err := s.Find(ctx, id)
	if err != nil {
		return true, err
	}
	return row.RevokedAt != nil || !row.ExpiresAt.After(time.Now()), nil
}
func (s *sessions) Revoke(ctx context.Context, id string) error {
	db, err := s.database.Session(ctx)
	if err != nil {
		return err
	}
	return translate(db.Model(&sessionModel{}).Where("id = ? AND revoked_at IS NULL", id).Update("revoked_at", time.Now().UTC()).Error)
}
