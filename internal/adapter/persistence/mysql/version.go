package mysql

import (
	"context"
	"fmt"
)

func bumpAssetVersion(ctx context.Context, database Database) error {
	db, err := database.Session(ctx)
	if err != nil {
		return err
	}

	result := db.Exec(
		"UPDATE data_versions SET version = version + 1 WHERE name = ?",
		"assets",
	)
	if result.Error != nil {
		return translate(result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("asset version record missing; run cmd/migrate")
	}

	return nil
}

func (repository *marketRepository) AssetVersion(ctx context.Context) (uint64, error) {
	db, err := repository.database.Session(ctx)
	if err != nil {
		return 0, err
	}

	var row struct {
		Version uint64
	}

	err = db.Table("data_versions").
		Select("version").
		Where("name = ?", "assets").
		Take(&row).Error
	if err != nil {
		return 0, translate(err)
	}

	return row.Version, nil
}