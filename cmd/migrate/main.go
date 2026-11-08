package main

import (
	"context"
	"depeg.local/platform/internal/bootstrap"
	"fmt"
	"os"
)

func main() {
	if err := run(context.Background(), bootstrap.Default()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context, factory bootstrap.Factory) error {
	settings, err := factory.Config().Load(ctx)
	if err != nil {
		return err
	}
	logger := factory.Logger(settings.Log)
	database, err := factory.MySQL(settings.MySQL, logger)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.Ping(ctx); err != nil {
		return fmt.Errorf("MySQL is unavailable; check MYSQL_DSN and the database service")
	}
	if err := database.Migrate(ctx); err != nil {
		return err
	}
	fmt.Println("MySQL migrations applied")
	return nil
}
