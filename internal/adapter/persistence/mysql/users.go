package mysql

import (
	"context"
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
	return translate(db.Create(&userModel{ID: user.ID, Username: user.Username, PasswordHash: user.PasswordHash, Disabled: user.Disabled}).Error)
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
	return auth.User{ID: row.ID, Username: row.Username, PasswordHash: row.PasswordHash, Disabled: row.Disabled}, nil
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
	return auth.User{ID: row.ID, Username: row.Username, PasswordHash: row.PasswordHash, Disabled: row.Disabled}, nil
}
