package mysql

import (
	"context"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/port"
	"time"
)

type sessions struct{ database Database }

func NewSessionStore(database Database) port.SessionStore { return &sessions{database: database} }
func (s *sessions) Save(ctx context.Context, session auth.Session) error {
	db, err := s.database.Session(ctx)
	if err != nil {
		return err
	}
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
		now := time.Now().UTC()
		result := db.Model(&sessionModel{}).Where("refresh_hash = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > ?", oldHash, next.UserID, now).Update("revoked_at", now)
		if result.Error != nil {
			return translate(result.Error)
		}
		if result.RowsAffected != 1 {
			return domain.ErrUnauthorized
		}
		return NewSessionStore(database).Save(ctx, next)
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
