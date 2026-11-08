package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	driver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"

	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/port"
)

type Database interface {
	port.Database
	Session(context.Context) (*gorm.DB, error)
	Transaction(context.Context, func(Database) error) error
	Migrate(context.Context) error
}
type client struct {
	database    *gorm.DB
	logger      port.Logger
	transaction bool
}

func New(settings config.MySQL, logger port.Logger) (Database, error) {
	dsn, err := driver.ParseDSN(settings.DSN)
	if err != nil || dsn.DBName == "" {
		return nil, fmt.Errorf("MYSQL_DSN must be a valid MySQL DSN with a database name")
	}
	dsn.ParseTime = true
	dsn.Loc = time.UTC
	if dsn.Timeout == 0 {
		dsn.Timeout = 5 * time.Second
	}
	if dsn.ReadTimeout == 0 {
		dsn.ReadTimeout = 10 * time.Second
	}
	if dsn.WriteTimeout == 0 {
		dsn.WriteTimeout = 10 * time.Second
	}
	db, err := gorm.Open(gormmysql.New(gormmysql.Config{DSN: dsn.FormatDSN(), SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, TranslateError: true, Logger: gormlog.Default.LogMode(gormlog.Silent), NowFunc: func() time.Time { return time.Now().UTC() }})
	if err != nil {
		return nil, fmt.Errorf("initialize MySQL: %w", domain.ErrUnavailable)
	}
	raw, err := db.DB()
	if err != nil {
		return nil, err
	}
	raw.SetMaxOpenConns(settings.MaxOpenConns)
	raw.SetMaxIdleConns(settings.MaxIdleConns)
	raw.SetConnMaxLifetime(settings.ConnMaxLifetime)
	return FromGORM(db, logger)
}

// FromGORM wraps an already initialized handle without exposing it to business services.
func FromGORM(db *gorm.DB, logger port.Logger) (Database, error) {
	if db == nil {
		return nil, domain.ErrUnavailable
	}
	return &client{database: db, logger: logger}, nil
}
func (c *client) Ping(ctx context.Context) error {
	raw, err := c.database.DB()
	if err != nil {
		return domain.ErrUnavailable
	}
	check, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := raw.PingContext(check); err != nil {
		return domain.ErrUnavailable
	}
	return nil
}
func (c *client) Session(ctx context.Context) (*gorm.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.database == nil {
		return nil, domain.ErrUnavailable
	}
	return c.database.WithContext(ctx), nil
}
func (c *client) Transaction(ctx context.Context, fn func(Database) error) error {
	if fn == nil {
		return domain.ErrInvalid
	}
	return c.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(&client{database: tx, logger: c.logger, transaction: true}) })
}
func (c *client) Close() error {
	if c.transaction {
		return nil
	}
	raw, err := c.database.DB()
	if err != nil {
		return err
	}
	return raw.Close()
}
func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrNotFound
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return domain.ErrConflict
	}
	var mysqlError *driver.MySQLError
	if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
		return domain.ErrConflict
	}
	return fmt.Errorf("%w: database operation", domain.ErrUnavailable)
}
