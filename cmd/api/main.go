package main

import (
	"context"
	"depeg.local/platform/internal/bootstrap"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	if err := run(ctx, bootstrap.Default()); err != nil {
		stop()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	stop()
}
func run(ctx context.Context, factory bootstrap.Factory) error {
	settings, err := factory.Config().Load(ctx)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
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

	rdb, err := factory.Redis(settings.Redis,logger)
	if err != nil {
		logger.Error(ctx,"failed to connect to redis", "error", err)
		return fmt.Errorf("failed to connect to redis: %w", err)
	}
	defer rdb.Close()
	assetCache := factory.AssetCache(rdb)
	users := factory.Users(database)
	markets := factory.MarketRepository(database)
	risks := factory.RiskRepository(database)
	tokens := factory.Tokens(settings.JWT)
	passwords := factory.Passwords()
	sessions := factory.Sessions(database)
	authentication := factory.AuthService(users, passwords, tokens, sessions)
	trends := factory.MarketService(markets,assetCache,logger)
	monitors := factory.MonitorService(factory.MonitorRepository(database), trends)
	riskSeries := factory.RiskService(risks)
	handlers := factory.Handlers(authentication, trends, riskSeries, monitors, database, logger)
	guard := factory.JWTGuard(authentication)
	server := factory.Server(settings.HTTP, handlers, guard, logger)
	return server.Run(ctx)
}
