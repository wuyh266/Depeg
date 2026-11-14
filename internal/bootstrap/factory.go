// Package bootstrap is the only production package that selects adapters.
// Entrypoints assemble their dependency graph through Factory.
package bootstrap

import (
	"depeg.local/platform/internal/adapter/cache/redis"
	"depeg.local/platform/internal/adapter/dataset/coingecko"
	"depeg.local/platform/internal/adapter/email"
	httpserver "depeg.local/platform/internal/adapter/http"
	"depeg.local/platform/internal/adapter/http/handler"
	"depeg.local/platform/internal/adapter/http/middleware"
	"depeg.local/platform/internal/adapter/logging"
	"depeg.local/platform/internal/adapter/persistence/mysql"
	"depeg.local/platform/internal/adapter/pipeline/python"
	"depeg.local/platform/internal/adapter/scheduler"
	"depeg.local/platform/internal/adapter/security/jwt"
	"depeg.local/platform/internal/adapter/security/password"
	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/port"
	authservice "depeg.local/platform/internal/service/auth"
	"depeg.local/platform/internal/service/ingestion"
	marketservice "depeg.local/platform/internal/service/market"
	monitorservice "depeg.local/platform/internal/service/monitor"
	riskservice "depeg.local/platform/internal/service/risk"
)

// Factory creates interface-valued dependencies; it is used only at startup.
// TODO: Replace placeholder adapters individually without moving wiring from main.
type Factory interface {
	Config() config.Loader
	Logger(settings config.Log) port.Logger
	MySQL(settings config.MySQL, logger port.Logger) (mysql.Database, error)
	Redis(settings config.Redis, logger port.Logger) (port.RedisClient, error)
	Users(database mysql.Database) port.UserRepository
	MarketRepository(database mysql.Database) port.MarketRepository
	MonitorRepository(database mysql.Database) port.MonitorRepository
	DatasetReader() port.DatasetReader
	DatasetRepository(database mysql.Database) port.DatasetRepository
	RiskRepository(database mysql.Database) port.RiskRepository
	Jobs(database mysql.Database) port.JobRepository
	Tokens(settings config.JWT) port.TokenManager
	Passwords() port.PasswordHasher
	Sessions(database mysql.Database) port.SessionStore
	RegistrationCodes(database mysql.Database) port.EmailCodeStore
	PasswordResetCodes(database mysql.Database) port.EmailCodeStore
	EmailCodeMailer(settings config.SMTP) port.EmailCodeMailer
	TrendCache(client port.RedisClient) port.TrendCache
	Leases(client port.RedisClient) port.LeaseStore
	Pipeline(settings config.Python, logger port.Logger) port.PipelineGateway
	AuthService(users port.UserRepository, passwords port.PasswordHasher, tokens port.TokenManager, sessions port.SessionStore, registrationCodes, resetCodes port.EmailCodeStore, mailer port.EmailCodeMailer, signingKey string) port.AuthService
	MarketService(repository port.MarketRepository, assets port.AssetCache, logger port.Logger) port.MarketService
	MonitorService(repository port.MonitorRepository, market port.MarketService) port.MonitorService
	RiskService(repository port.RiskRepository) port.RiskService
	Ingestion(market port.MarketRepository, risks port.RiskRepository, jobs port.JobRepository, leases port.LeaseStore, pipeline port.PipelineGateway, logger port.Logger) port.IngestionService
	Handlers(auth port.AuthService, market port.MarketService, risks port.RiskService, monitors port.MonitorService, database port.Database, logger port.Logger) handler.Handlers
	JWTGuard(auth port.AuthService) middleware.JWTGuard
	Server(settings config.HTTP, handlers handler.Handlers, guard middleware.JWTGuard, logger port.Logger) port.Server
	Scheduler(settings config.Worker, ingestion port.IngestionService, logger port.Logger) port.Scheduler
	AssetCache(client port.RedisClient) port.AssetCache
}

type factory struct{}

// Default selects the current adapters without exposing their concrete types.
func Default() Factory { return &factory{} }

func (*factory) Config() config.Loader {
	return config.NewLoader()
}

func (*factory) Logger(settings config.Log) port.Logger {
	return logging.New(settings)
}

func (*factory) MySQL(settings config.MySQL, logger port.Logger) (mysql.Database, error) {
	return mysql.New(settings, logger)
}

func (*factory) Redis(settings config.Redis, logger port.Logger) (port.RedisClient, error) {
	return redis.New(settings, logger)
}

func (*factory) Users(database mysql.Database) port.UserRepository {
	return mysql.NewUserRepository(database)
}

func (*factory) MarketRepository(database mysql.Database) port.MarketRepository {
	return mysql.NewMarketRepository(database)
}

func (*factory) MonitorRepository(database mysql.Database) port.MonitorRepository {
	return mysql.NewMonitorRepository(database)
}
func (*factory) MonitorService(repository port.MonitorRepository, market port.MarketService) port.MonitorService {
	return monitorservice.New(repository, market)
}

func (*factory) DatasetReader() port.DatasetReader { return coingecko.New() }
func (*factory) DatasetRepository(database mysql.Database) port.DatasetRepository {
	return mysql.NewDatasetRepository(database)
}

func (*factory) RiskRepository(database mysql.Database) port.RiskRepository {
	return mysql.NewRiskRepository(database)
}

func (*factory) Jobs(database mysql.Database) port.JobRepository {
	return mysql.NewJobRepository(database)
}

func (*factory) Tokens(settings config.JWT) port.TokenManager {
	return jwt.New(settings)
}

func (*factory) Passwords() port.PasswordHasher {
	return password.New()
}

func (*factory) Sessions(database mysql.Database) port.SessionStore {
	return mysql.NewSessionStore(database)
}

func (*factory) RegistrationCodes(database mysql.Database) port.EmailCodeStore {
	return mysql.NewRegistrationCodes(database)
}

func (*factory) PasswordResetCodes(database mysql.Database) port.EmailCodeStore {
	return mysql.NewPasswordResetCodes(database)
}

func (*factory) EmailCodeMailer(settings config.SMTP) port.EmailCodeMailer {
	return email.New(settings)
}

func (*factory) TrendCache(client port.RedisClient) port.TrendCache {
	return redis.NewTrendCache(client)
}

func (*factory) Leases(client port.RedisClient) port.LeaseStore {
	return redis.NewLeaseStore(client)
}

func (*factory) Pipeline(settings config.Python, logger port.Logger) port.PipelineGateway {
	return python.New(settings, logger)
}

func (*factory) AuthService(users port.UserRepository, passwords port.PasswordHasher, tokens port.TokenManager, sessions port.SessionStore, registrationCodes, resetCodes port.EmailCodeStore, mailer port.EmailCodeMailer, signingKey string) port.AuthService {
	return authservice.New(users, passwords, tokens, sessions, registrationCodes, resetCodes, mailer, signingKey)
}

func (*factory) MarketService(repository port.MarketRepository, assetscache port.AssetCache, logger port.Logger) port.MarketService {
	return marketservice.New(repository, assetscache, logger)
}

func (*factory) RiskService(repository port.RiskRepository) port.RiskService {
	return riskservice.New(repository)
}

func (*factory) Ingestion(market port.MarketRepository, risks port.RiskRepository, jobs port.JobRepository, leases port.LeaseStore, pipeline port.PipelineGateway, logger port.Logger) port.IngestionService {
	return ingestion.New(market, risks, jobs, leases, pipeline, logger)
}

func (*factory) Handlers(auth port.AuthService, market port.MarketService, risks port.RiskService, monitors port.MonitorService, database port.Database, logger port.Logger) handler.Handlers {
	return handler.New(auth, market, risks, monitors, database, logger)
}

func (*factory) JWTGuard(auth port.AuthService) middleware.JWTGuard {
	return middleware.NewJWT(auth)
}

func (*factory) Server(settings config.HTTP, handlers handler.Handlers, guard middleware.JWTGuard, logger port.Logger) port.Server {
	return httpserver.New(settings, handlers, guard, logger)
}

func (*factory) Scheduler(settings config.Worker, ingestion port.IngestionService, logger port.Logger) port.Scheduler {
	return scheduler.New(settings, ingestion, logger)
}

func (*factory) AssetCache(client port.RedisClient) port.AssetCache {
	return redis.NewAssetCache(client)
}
