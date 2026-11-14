package main

import (
	"context"
	"depeg.local/platform/internal/bootstrap"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"depeg.local/platform/internal/port"
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

	var assetCache port.AssetCache

	if settings.Redis.Address != "" {
		redisClient, err := factory.Redis(settings.Redis, logger)
		if err != nil {
			if logger != nil {
				logger.Error(ctx, "Redis initialization failed; using MySQL", "error", err)
			}
		} else {
			defer redisClient.Close()
			assetCache = factory.AssetCache(redisClient)
		}
	}
	users := factory.Users(database)
	markets := factory.MarketRepository(database)
	risks := factory.RiskRepository(database)
	tokens := factory.Tokens(settings.JWT)
	passwords := factory.Passwords()
	sessions := factory.Sessions(database)
	registrationCodes := factory.RegistrationCodes(database)
	resetCodes := factory.PasswordResetCodes(database)
	mailer := factory.EmailCodeMailer(settings.SMTP)
	authentication := factory.AuthService(users, passwords, tokens, sessions, registrationCodes, resetCodes, mailer, settings.JWT.SigningKey)
	trends := factory.MarketService(markets, assetCache, logger)
	monitors := factory.MonitorService(factory.MonitorRepository(database), trends)
	riskSeries := factory.RiskService(risks)
	handlers := factory.Handlers(authentication, trends, riskSeries, monitors, database, logger)
	guard := factory.JWTGuard(authentication)
	server := factory.Server(settings.HTTP, handlers, guard, logger)
	return server.Run(ctx)
}
