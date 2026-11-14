package mysql

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/port"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type emailCodeModel struct {
	Email        string    `gorm:"size:254;primaryKey"`
	CodeHash     string    `gorm:"size:64;not null"`
	ExpiresAt    time.Time `gorm:"index;not null"`
	ResendAt     time.Time `gorm:"not null"`
	AttemptsLeft int       `gorm:"not null"`
}

func (emailCodeModel) TableName() string { return "auth_registration_codes" }

type passwordResetCodeModel emailCodeModel

func (passwordResetCodeModel) TableName() string { return "auth_password_reset_codes" }

type emailCodeSendModel struct {
	SourceHash string    `gorm:"size:64;primaryKey"`
	ExpiresAt  time.Time `gorm:"index;not null"`
	Sent       int       `gorm:"not null"`
}

func (emailCodeSendModel) TableName() string { return "auth_registration_sends" }

type emailCodes struct {
	database Database
	table    string
}

func NewRegistrationCodes(database Database) port.EmailCodeStore {
	return &emailCodes{database: database, table: (emailCodeModel{}).TableName()}
}

func NewPasswordResetCodes(database Database) port.EmailCodeStore {
	return &emailCodes{database: database, table: (passwordResetCodeModel{}).TableName()}
}

func (r *emailCodes) Reserve(ctx context.Context, email, codeHash, sourceHash string) error {
	db, err := r.database.Session(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := db.Table(r.table).Where("expires_at <= ?", now).Delete(&emailCodeModel{}).Error; err != nil {
		return translate(err)
	}
	if err := db.Where("expires_at <= ?", now).Delete(&emailCodeSendModel{}).Error; err != nil {
		return translate(err)
	}

	return r.database.Transaction(ctx, func(tx Database) error {
		db, err := tx.Session(ctx)
		if err != nil {
			return err
		}
		bucket := emailCodeSendModel{SourceHash: sourceHash, ExpiresAt: now.Add(auth.EmailSendWindow)}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&bucket).Error; err != nil {
			return translate(err)
		}
		sent := db.Model(&emailCodeSendModel{}).
			Where("source_hash = ? AND sent < ?", sourceHash, auth.EmailSendLimit).
			UpdateColumn("sent", gorm.Expr("sent + 1"))
		if sent.Error != nil {
			return translate(sent.Error)
		}
		if sent.RowsAffected != 1 {
			return auth.ErrEmailSendLimit
		}

		// Create a row for a new address, then conditionally replace its code.
		first := emailCodeModel{Email: email, ExpiresAt: now, ResendAt: now}
		if err := db.Table(r.table).Clauses(clause.OnConflict{DoNothing: true}).Create(&first).Error; err != nil {
			return translate(err)
		}
		updated := db.Table(r.table).
			Where("email = ? AND resend_at <= ?", email, now).
			Updates(map[string]any{
				"code_hash": codeHash, "expires_at": now.Add(auth.EmailCodeTTL),
				"resend_at": now.Add(auth.EmailCodeCooldown), "attempts_left": auth.EmailCodeMaxAttempts,
			})
		if updated.Error != nil {
			return translate(updated.Error)
		}
		if updated.RowsAffected != 1 {
			return auth.ErrEmailCodeCooldown
		}
		return nil
	})
}

func (r *emailCodes) Use(ctx context.Context, email, codeHash string, apply func(port.UserRepository) error) error {
	var incorrect bool
	err := r.database.Transaction(ctx, func(tx Database) error {
		db, err := tx.Session(ctx)
		if err != nil {
			return err
		}
		var code emailCodeModel
		err = db.Table(r.table).Clauses(clause.Locking{Strength: "UPDATE"}).Where("email = ?", email).Take(&code).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return auth.ErrInvalidEmailCode
		}
		if err != nil {
			return translate(err)
		}
		if code.AttemptsLeft <= 0 || !code.ExpiresAt.After(time.Now()) {
			return auth.ErrInvalidEmailCode
		}
		if subtle.ConstantTimeCompare([]byte(code.CodeHash), []byte(codeHash)) != 1 {
			incorrect = true
			return translate(db.Table(r.table).Where("email = ?", email).UpdateColumn("attempts_left", gorm.Expr("attempts_left - 1")).Error)
		}
		if err := apply(NewUserRepository(tx)); err != nil {
			return err
		}
		// Keep the resend cooldown after consuming a code.
		return translate(db.Table(r.table).Where("email = ?", email).Updates(map[string]any{"code_hash": "", "attempts_left": 0}).Error)
	})
	if err == nil && incorrect {
		return auth.ErrInvalidEmailCode
	}
	return err
}
