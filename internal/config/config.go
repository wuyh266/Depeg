package config

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Settings struct {
	HTTP   HTTP   `yaml:"http"`
	MySQL  MySQL  `yaml:"mysql"`
	Redis  Redis  `yaml:"redis"`
	JWT    JWT    `yaml:"jwt"`
	SMTP   SMTP   `yaml:"smtp"`
	Python Python `yaml:"python"`
	Worker Worker `yaml:"worker"`
	Log    Log    `yaml:"log"`
}
type HTTP struct {
	Address         string        `yaml:"address"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	AllowedOrigins  []string      `yaml:"allowed_origins"`
}
type MySQL struct {
	DSN             string        `yaml:"dsn"`
	MaxOpenConns    int           `yaml:"max_open_conns"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
}
type Redis struct {
	Address  string        `yaml:"address"`
	Username string        `yaml:"username"`
	Password string        `yaml:"password"`
	Database int           `yaml:"database"`
	Timeout  time.Duration `yaml:"timeout"`
}
type JWT struct {
	Issuer     string        `yaml:"issuer"`
	Audience   string        `yaml:"audience"`
	SigningKey string        `yaml:"signing_key"`
	AccessTTL  time.Duration `yaml:"access_ttl"`
	RefreshTTL time.Duration `yaml:"refresh_ttl"`
}
type SMTP struct {
	Address  string `yaml:"address"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	From     string `yaml:"from"`
	TLSMode  string `yaml:"tls_mode"`
}

func (s *SMTP) validate() error {
	if s.Address == "" {
		if s.Username != "" || s.Password != "" || s.From != "" {
			return fmt.Errorf("SMTP_ADDRESS is required when SMTP credentials or sender are set")
		}
		return nil
	}
	host, port, err := net.SplitHostPort(s.Address)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || host == "" || strings.ContainsAny(host, " \t\r\n") || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("SMTP_ADDRESS must be host:port")
	}
	if (s.Username == "") != (s.Password == "") {
		return fmt.Errorf("SMTP_USERNAME and SMTP_PASSWORD must be configured together")
	}
	if s.From == "" {
		s.From = s.Username
	}
	from, err := mail.ParseAddress(s.From)
	if err != nil || from.Address != s.From || strings.ContainsAny(s.From, "\r\n") {
		return fmt.Errorf("SMTP_FROM must be a single email address")
	}
	switch s.TLSMode {
	case "tls", "starttls":
	case "none":
		if ip := net.ParseIP(host); !ip.IsLoopback() || s.Username != "" {
			return fmt.Errorf("SMTP_TLS_MODE=none requires a loopback IP and no SMTP credentials")
		}
	default:
		return fmt.Errorf("SMTP_TLS_MODE must be tls, starttls, or none")
	}
	return nil
}

type Python struct {
	BaseURL string        `yaml:"base_url"`
	Timeout time.Duration `yaml:"timeout"`
}
type Worker struct {
	Interval  time.Duration `yaml:"interval"`
	LeaseTTL  time.Duration `yaml:"lease_ttl"`
	BatchSize int           `yaml:"batch_size"`
}
type Log struct {
	Level string `yaml:"level"`
}
type Loader interface {
	Load(context.Context) (Settings, error)
}
type loader struct{}

func NewLoader() Loader { return &loader{} }

func (*loader) Load(ctx context.Context) (Settings, error) {
	return load(ctx, false)
}

// LoadDatabase lets offline commands read MySQL and logging settings without
// needing the API's authentication, HTTP or Redis settings.
func LoadDatabase(ctx context.Context) (MySQL, Log, error) {
	settings, err := load(ctx, true)
	return settings.MySQL, settings.Log, err
}

func load(ctx context.Context, databaseOnly bool) (Settings, error) {
	if err := ctx.Err(); err != nil {
		return Settings{}, err
	}
	settings := Settings{
		HTTP:  HTTP{Address: "127.0.0.1:8080", ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, ShutdownTimeout: 15 * time.Second},
		MySQL: MySQL{MaxOpenConns: 20, MaxIdleConns: 5, ConnMaxLifetime: 30 * time.Minute},
		JWT:   JWT{Issuer: "depeg-platform", Audience: "depeg-web", AccessTTL: 15 * time.Minute, RefreshTTL: 7 * 24 * time.Hour},
		SMTP:  SMTP{TLSMode: "tls"},
		Log:   Log{Level: "info"},
		Redis: Redis{Timeout: 300 * time.Millisecond},
	}
	values := map[string]string{}
	envFile := os.Getenv("ENV_FILE")
	explicitEnv := envFile != ""
	if envFile == "" {
		envFile = ".env"
	}
	file, err := os.Open(envFile)
	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 64*1024)
		line := 0
		keyPattern := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
		for scanner.Scan() {
			line++
			raw := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
			if raw == "" || strings.HasPrefix(raw, "#") {
				continue
			}
			key, value, ok := strings.Cut(strings.TrimPrefix(raw, "export "), "=")
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if !ok || !keyPattern.MatchString(key) {
				return Settings{}, fmt.Errorf("invalid environment file at line %d", line)
			}
			if strings.HasPrefix(value, "\"") {
				parsed, err := strconv.Unquote(value)
				if err != nil {
					return Settings{}, fmt.Errorf("invalid quoted environment value at line %d", line)
				}
				value = parsed
			} else if strings.HasPrefix(value, "'") {
				if len(value) < 2 || !strings.HasSuffix(value, "'") {
					return Settings{}, fmt.Errorf("invalid environment value at line %d", line)
				}
				value = value[1 : len(value)-1]
			}
			values[key] = value
		}
		if err := scanner.Err(); err != nil {
			return Settings{}, fmt.Errorf("read environment file: %w", err)
		}
	} else if explicitEnv || !os.IsNotExist(err) {
		return Settings{}, fmt.Errorf("read environment file: %w", err)
	}
	lookup := func(key string) string {
		if value, ok := os.LookupEnv(key); ok {
			return value
		}
		return values[key]
	}
	configFile := lookup("CONFIG_FILE")
	explicitConfig := configFile != ""
	if configFile == "" {
		configFile = "configs/config.yaml"
	}
	content, err := os.ReadFile(configFile)
	if err == nil {
		expanded := os.Expand(string(content), lookup)
		decoder := yaml.NewDecoder(strings.NewReader(expanded))
		decoder.KnownFields(true)
		if err := decoder.Decode(&settings); err != nil {
			return Settings{}, fmt.Errorf("invalid YAML configuration")
		}
	} else if explicitConfig || !os.IsNotExist(err) {
		return Settings{}, fmt.Errorf("read configuration: %w", err)
	}
	override := func(key string, field *string) {
		if value, ok := os.LookupEnv(key); ok {
			*field = value
		} else if value, ok := values[key]; ok {
			*field = value
		}
	}
	override("MYSQL_DSN", &settings.MySQL.DSN)
	override("LOG_LEVEL", &settings.Log.Level)
	if settings.MySQL.DSN == "" {
		return Settings{}, fmt.Errorf("MYSQL_DSN is required; configure the project .env")
	}
	if settings.MySQL.MaxOpenConns < 1 || settings.MySQL.MaxIdleConns < 0 || settings.MySQL.MaxIdleConns > settings.MySQL.MaxOpenConns || settings.MySQL.ConnMaxLifetime <= 0 {
		return Settings{}, fmt.Errorf("invalid database pool limits")
	}
	switch settings.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return Settings{}, fmt.Errorf("invalid log level")
	}
	if databaseOnly {
		return settings, nil
	}
	override("HTTP_ADDRESS", &settings.HTTP.Address)
	override("JWT_ISSUER", &settings.JWT.Issuer)
	override("JWT_AUDIENCE", &settings.JWT.Audience)
	override("JWT_SIGNING_KEY", &settings.JWT.SigningKey)
	override("SMTP_ADDRESS", &settings.SMTP.Address)
	override("SMTP_USERNAME", &settings.SMTP.Username)
	override("SMTP_PASSWORD", &settings.SMTP.Password)
	override("SMTP_FROM", &settings.SMTP.From)
	override("SMTP_TLS_MODE", &settings.SMTP.TLSMode)
	override("REDIS_ADDRESS", &settings.Redis.Address)
	override("REDIS_USERNAME", &settings.Redis.Username)
	override("REDIS_PASSWORD", &settings.Redis.Password)

	if value := lookup("REDIS_DATABASE"); value != "" {
		database, err := strconv.Atoi(value)
		if err != nil || database < 0 {
			return Settings{}, fmt.Errorf("REDIS_DATABASE must be a nonnegative integer")
		}
		settings.Redis.Database = database
	}

	if value := lookup("REDIS_TIMEOUT"); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil || timeout <= 0 {
			return Settings{}, fmt.Errorf("REDIS_TIMEOUT must be a positive duration")
		}
		settings.Redis.Timeout = timeout
	}
	if value := lookup("ALLOWED_ORIGINS"); value != "" {
		settings.HTTP.AllowedOrigins = strings.Split(value, ",")
	}
	for key, field := range map[string]*time.Duration{"JWT_ACCESS_TTL": &settings.JWT.AccessTTL, "JWT_REFRESH_TTL": &settings.JWT.RefreshTTL} {
		if value := lookup(key); value != "" {
			duration, err := time.ParseDuration(value)
			if err != nil {
				return Settings{}, fmt.Errorf("%s must be a duration", key)
			}
			*field = duration
		}
	}
	if len(settings.JWT.SigningKey) < 32 {
		return Settings{}, fmt.Errorf("JWT_SIGNING_KEY must contain at least 32 bytes")
	}
	if settings.JWT.Issuer == "" || settings.JWT.Audience == "" {
		return Settings{}, fmt.Errorf("JWT issuer and audience are required")
	}
	if settings.JWT.AccessTTL <= 0 || settings.JWT.AccessTTL > 24*time.Hour || settings.JWT.RefreshTTL <= settings.JWT.AccessTTL || settings.JWT.RefreshTTL > 90*24*time.Hour {
		return Settings{}, fmt.Errorf("invalid JWT token lifetimes")
	}
	if _, _, err := net.SplitHostPort(settings.HTTP.Address); err != nil {
		return Settings{}, fmt.Errorf("HTTP address must be host:port")
	}
	if settings.HTTP.ReadTimeout <= 0 || settings.HTTP.WriteTimeout <= 0 || settings.HTTP.IdleTimeout <= 0 || settings.HTTP.ShutdownTimeout <= 0 {
		return Settings{}, fmt.Errorf("HTTP timeouts must be positive")
	}
	for i, origin := range settings.HTTP.AllowedOrigins {
		origin = strings.TrimSpace(origin)
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
			return Settings{}, fmt.Errorf("allowed origins must be explicit HTTP origins")
		}
		settings.HTTP.AllowedOrigins[i] = origin
	}
	if err := settings.SMTP.validate(); err != nil {
		return Settings{}, err
	}
	return settings, nil
}
