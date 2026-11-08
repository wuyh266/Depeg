package config_test

import (
	"context"
	"depeg.local/platform/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigurationLoadsDotEnvAndYAMLWithEnvironmentOverride(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{"MYSQL_DSN", "JWT_SIGNING_KEY", "JWT_ACCESS_TTL", "JWT_REFRESH_TTL", "ALLOWED_ORIGINS", "LOG_LEVEL"} {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
	envFile := filepath.Join(dir, "local.env")
	content := "MYSQL_DSN=user:password@tcp(localhost:3306)/depeg\nJWT_SIGNING_KEY=" + strings.Repeat("k", 32) + "\nHTTP_ADDRESS=127.0.0.1:8081\n"
	if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENV_FILE", envFile)
	t.Setenv("CONFIG_FILE", filepath.Join(dir, "config.yaml"))
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("http:\n  address: 127.0.0.1:8082\n  read_timeout: 12s\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HTTP_ADDRESS", "127.0.0.1:8099")
	settings, err := config.NewLoader().Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if settings.HTTP.Address != "127.0.0.1:8099" || settings.HTTP.ReadTimeout != 12*time.Second || settings.JWT.SigningKey != strings.Repeat("k", 32) {
		t.Fatalf("unexpected settings: address=%s timeout=%s", settings.HTTP.Address, settings.HTTP.ReadTimeout)
	}
}
func TestConfigurationRequiresSecretsAndRejectsWildcardOrigins(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "empty.env")
	if err := os.WriteFile(envFile, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENV_FILE", envFile)
	yamlFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(yamlFile, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", yamlFile)
	t.Setenv("MYSQL_DSN", "")
	t.Setenv("JWT_SIGNING_KEY", "")
	_, err := config.NewLoader().Load(context.Background())
	if err == nil {
		t.Fatal("missing DSN accepted")
	}
	t.Setenv("MYSQL_DSN", "user:password@tcp(localhost:3306)/depeg")
	t.Setenv("JWT_SIGNING_KEY", strings.Repeat("s", 32))
	t.Setenv("ALLOWED_ORIGINS", "*")
	_, err = config.NewLoader().Load(context.Background())
	if err == nil {
		t.Fatal("wildcard origin accepted")
	}
}
