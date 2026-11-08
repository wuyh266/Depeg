package httpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	httpserver "depeg.local/platform/internal/adapter/http"
	"depeg.local/platform/internal/adapter/http/handler"
	"depeg.local/platform/internal/adapter/http/middleware"
	"depeg.local/platform/internal/adapter/logging"
	"depeg.local/platform/internal/adapter/persistence/mysql"
	jwtadapter "depeg.local/platform/internal/adapter/security/jwt"
	"depeg.local/platform/internal/adapter/security/password"
	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/domain/dataset"
	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/identity"
	"depeg.local/platform/internal/importcsv"
	"depeg.local/platform/internal/port"
	authservice "depeg.local/platform/internal/service/auth"
	marketservice "depeg.local/platform/internal/service/market"
	monitorservice "depeg.local/platform/internal/service/monitor"
	riskservice "depeg.local/platform/internal/service/risk"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

type fixture struct {
	handler    http.Handler
	database   mysql.Database
	repository port.MarketRepository
	settings   config.Settings
}

func setup(t *testing.T) *fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "backend.sqlite")), &gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	if err := db.AutoMigrate(mysql.SchemaModels()...); err != nil {
		t.Fatal(err)
	}
	settings := config.Settings{HTTP: config.HTTP{Address: "127.0.0.1:0", ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, ShutdownTimeout: time.Second, AllowedOrigins: []string{"http://localhost:5173"}}, JWT: config.JWT{Issuer: "tests", Audience: "test-web", SigningKey: strings.Repeat("s", 32), AccessTTL: 15 * time.Minute, RefreshTTL: time.Hour}, Log: config.Log{Level: "error"}}
	logger := logging.New(settings.Log)
	database, err := mysql.FromGORM(db, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository := mysql.NewMarketRepository(database)
	authentication := authservice.New(mysql.NewUserRepository(database), password.New(), jwtadapter.New(settings.JWT), mysql.NewSessionStore(database))
	markets := marketservice.New(repository,nil,nil)
	handlers := handler.New(authentication, markets, riskservice.New(mysql.NewRiskRepository(database)), monitorservice.New(mysql.NewMonitorRepository(database), markets), database, logger)
	server := httpserver.New(settings.HTTP, handlers, middleware.NewJWT(authentication), logger)
	return &fixture{handler: server.Handler(), database: database, repository: repository, settings: settings}
}
func request(t *testing.T, router http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		data = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(method, path, data)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	result := httptest.NewRecorder()
	router.ServeHTTP(result, req)
	return result
}
func status(t *testing.T, r *httptest.ResponseRecorder, want int) {
	t.Helper()
	if r.Code != want {
		t.Fatalf("status=%d want=%d body=%s", r.Code, want, r.Body.String())
	}
	if r.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing request id")
	}
}
func pair(t *testing.T, r *httptest.ResponseRecorder) auth.TokenPair {
	t.Helper()
	var body struct {
		Data auth.TokenPair `json:"data"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.AccessToken == "" || body.Data.RefreshToken == "" {
		t.Fatal("missing tokens")
	}
	return body.Data
}
func login(t *testing.T, f *fixture) auth.TokenPair {
	t.Helper()
	credentials := map[string]string{"username": "researcher", "password": "correct-password"}
	status(t, request(t, f.handler, "POST", "/api/v1/auth/register", "", credentials), 201)
	r := request(t, f.handler, "POST", "/api/v1/auth/login", "", credentials)
	status(t, r, 200)
	return pair(t, r)
}
func TestBackendAuthenticationAndHistoricalTrends(t *testing.T) {
	f := setup(t)
	credentials := map[string]string{"username": "Researcher", "password": "correct-password"}
	registration := request(t, f.handler, "POST", "/api/v1/auth/register", "", credentials)
	status(t, registration, 201)
	if strings.Contains(registration.Body.String(), "password") {
		t.Fatal("registration leaked password fields")
	}
	status(t, request(t, f.handler, "POST", "/api/v1/auth/register", "", credentials), 409)
	status(t, request(t, f.handler, "POST", "/api/v1/auth/login", "", map[string]string{"username": "researcher", "password": "wrong-password"}), 401)
	r := request(t, f.handler, "POST", "/api/v1/auth/login", "", credentials)
	status(t, r, 200)
	tokens := pair(t, r)
	for _, token := range []string{"", "forged.token.value", tokens.RefreshToken} {
		status(t, request(t, f.handler, "GET", "/api/v1/assets", token, nil), 401)
	}
	status(t, request(t, f.handler, "GET", "/api/v1/auth/me", tokens.AccessToken, nil), 200)
	db, _ := f.database.Session(context.Background())
	var row struct{ RefreshHash string }
	if err := db.Table("auth_sessions").Select("refresh_hash").Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.RefreshHash != identity.Digest(tokens.RefreshToken) || row.RefreshHash == tokens.RefreshToken {
		t.Fatal("refresh token must be stored as a digest")
	}
	asset := market.Asset{ID: "usdc-ethereum", Symbol: "USDC", Chain: "ethereum", QuoteCurrency: "USD", AnchorPrice: decimalPointer("1")}
	csv := "timestamp,datetime,open,high,low,close,volume,exchange\n1672531200,2023-01-01 00:00:00,1,1.01,0.99,1,12,okx\n1672531260,2023-01-01 00:01:00,1,1.01,0.99,0.999,15,okx\n"
	candles, err := importcsv.Read(context.Background(), strings.NewReader(csv), asset, "paper-csv", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.repository.Import(context.Background(), asset, candles); err != nil {
		t.Fatal(err)
	}
	if err := f.repository.Import(context.Background(), asset, candles); err != nil {
		t.Fatal(err)
	}
	extra := candles[0]
	extra.Source = "paper-csv:coinbase"
	if err := f.repository.Import(context.Background(), asset, []market.Candle{extra}); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/trends?asset_id=usdc-ethereum&from=2023-01-01T00:00:00Z&to=2023-01-01T00:02:00Z&interval=1m&limit=1"
	cursor := ""
	seen := map[string]bool{}
	count := 0
	for page := 0; page < 4; page++ {
		path := base
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		response := request(t, f.handler, "GET", path, tokens.AccessToken, nil)
		status(t, response, 200)
		var body struct {
			Data market.Trend `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		for _, candle := range body.Data.Candles {
			key := candle.Time.Format(time.RFC3339) + candle.Source
			if seen[key] {
				t.Fatal("pagination duplicated a candle")
			}
			seen[key] = true
			count++
		}
		if body.Data.NextCursor == "" {
			break
		}
		cursor = body.Data.NextCursor
	}
	if count != 3 {
		t.Fatalf("expected three idempotent, source-distinct candles; got %d", count)
	}
	status(t, request(t, f.handler, "GET", base+"&cursor="+url.QueryEscape(cursor)+"&source=other", tokens.AccessToken, nil), 400)
	for _, bad := range []string{"/api/v1/trends?asset_id=usdc-ethereum&limit=0", "/api/v1/trends?asset_id=usdc-ethereum&limit=2001", "/api/v1/trends?asset_id=x%27%20OR%201=1", "/api/v1/trends?asset_id=usdc-ethereum&from=2023-01-01T00:00:00Z&to=2023-03-01T00:00:00Z", "/api/v1/trends?asset_id=usdc-ethereum&cursor=bad"} {
		status(t, request(t, f.handler, "GET", bad, tokens.AccessToken, nil), 400)
	}
	status(t, request(t, f.handler, "GET", "/api/v1/trends?asset_id=missing", tokens.AccessToken, nil), 404)
	status(t, request(t, f.handler, "GET", "/api/v1/risks?asset_id=usdc-ethereum", tokens.AccessToken, nil), 503)
	// Reconstruct services against the same database: authentication survives a process restart.
	again := authservice.New(mysql.NewUserRepository(f.database), password.New(), jwtadapter.New(f.settings.JWT), mysql.NewSessionStore(f.database))
	if _, err := again.Authenticate(context.Background(), tokens.AccessToken); err != nil {
		t.Fatal(err)
	}
	refreshed := request(t, f.handler, "POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": tokens.RefreshToken})
	status(t, refreshed, 200)
	next := pair(t, refreshed)
	status(t, request(t, f.handler, "POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": tokens.RefreshToken}), 401)
	status(t, request(t, f.handler, "GET", "/api/v1/auth/me", tokens.AccessToken, nil), 401)
	status(t, request(t, f.handler, "GET", "/api/v1/auth/me", next.AccessToken, nil), 200)
	status(t, request(t, f.handler, "POST", "/api/v1/auth/logout", next.AccessToken, nil), 204)
	status(t, request(t, f.handler, "GET", "/api/v1/auth/me", next.AccessToken, nil), 401)
	status(t, request(t, f.handler, "POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": next.RefreshToken}), 401)
	status(t, request(t, f.handler, "GET", "/healthz", "", nil), 200)
	status(t, request(t, f.handler, "GET", "/readyz", "", nil), 200)
}
func TestDatabaseRollbackAndReadinessFailure(t *testing.T) {
	f := setup(t)
	tokens := login(t, f)
	err := f.database.Transaction(context.Background(), func(tx mysql.Database) error {
		if err := mysql.NewMarketRepository(tx).Import(context.Background(), market.Asset{ID: "rolled-back", Symbol: "TEST", QuoteCurrency: "USD", AnchorPrice: decimalPointer("1")}, nil); err != nil {
			return err
		}
		return errors.New("rollback")
	})
	if err == nil {
		t.Fatal("transaction unexpectedly committed")
	}
	assets, err := f.repository.ListAssets(context.Background())
	if err != nil || len(assets) != 0 {
		t.Fatalf("rollback failed: %+v %v", assets, err)
	}
	_ = f.database.Close()
	status(t, request(t, f.handler, "GET", "/readyz", "", nil), 503)
	status(t, request(t, f.handler, "GET", "/api/v1/auth/me", tokens.AccessToken, nil), 503)
}
func TestDisabledAccountsAndRequestBoundaries(t *testing.T) {
	f := setup(t)
	tokens := login(t, f)
	raw, _ := f.database.Session(context.Background())
	if err := raw.Table("users").Where("username = ?", "researcher").Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	status(t, request(t, f.handler, "GET", "/api/v1/auth/me", tokens.AccessToken, nil), 401)
	status(t, request(t, f.handler, "POST", "/api/v1/auth/login", "", map[string]string{"username": "researcher", "password": "correct-password"}), 401)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	r := httptest.NewRecorder()
	f.handler.ServeHTTP(r, req)
	status(t, r, 400)
	req = httptest.NewRequest("OPTIONS", "/api/v1/trends", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	r = httptest.NewRecorder()
	f.handler.ServeHTTP(r, req)
	status(t, r, 204)
	req = httptest.NewRequest("GET", "/healthz", nil)
	req.Header.Set("Origin", "https://untrusted.invalid")
	r = httptest.NewRecorder()
	f.handler.ServeHTTP(r, req)
	status(t, r, 403)
	status(t, request(t, f.handler, "GET", "/missing", "", nil), 404)
}
func TestServerRunStopsOnCancellation(t *testing.T) {
	f := setup(t)
	logger := logging.New(f.settings.Log)
	authentication := authservice.New(mysql.NewUserRepository(f.database), password.New(), jwtadapter.New(f.settings.JWT), mysql.NewSessionStore(f.database))
	markets := marketservice.New(f.repository,nil,nil)
	handlers := handler.New(authentication, markets, riskservice.New(mysql.NewRiskRepository(f.database)), monitorservice.New(mysql.NewMonitorRepository(f.database), markets), f.database, logger)
	server := httpserver.New(f.settings.HTTP, handlers, middleware.NewJWT(authentication), logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	time.AfterFunc(50*time.Millisecond, cancel)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not shut down")
	}
}

func decimalPointer(value string) *string { return &value }

func TestImportedSnapshotsAndNullableCandles(t *testing.T) {
	f := setup(t)
	tokens := login(t, f)
	ctx := context.Background()
	day := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	a := market.Asset{ID: "coingecko.usd-coin", Symbol: "USDC", QuoteCurrency: "USD"}
	closed := day.Add(time.Hour)
	batch := dataset.Batch{Asset: &a, Snapshots: []market.Snapshot{{AssetID: a.ID, Source: "coingecko:market-chart", Time: day, PriceUSD: "1", QualityFlags: []string{"market_cap_usd_invalid"}, RawValues: `{"market_cap_usd":"-1"}`}}, Candles: []market.Candle{{AssetID: a.ID, Source: "coingecko:ohlc", Time: day, Interval: time.Hour, Open: "1", High: "1", Low: "1", Close: "1", Volume: nil, Quality: "historical", SourceTime: &closed, RawValues: `{"volume":null}`}}}
	if err := mysql.NewDatasetRepository(f.database).Import(ctx, func(sink port.DatasetSink) error { return sink.Write(ctx, batch) }); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/market-snapshots?asset_id=coingecko.usd-coin&from=2023-01-01T00:00:00Z&to=2023-01-02T00:00:00Z"
	status(t, request(t, f.handler, "GET", path, "", nil), 401)
	status(t, request(t, f.handler, "GET", path, tokens.RefreshToken, nil), 401)
	response := request(t, f.handler, "GET", path, tokens.AccessToken, nil)
	status(t, response, 200)
	var result struct {
		Data market.SnapshotSeries `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data.Points) != 1 || result.Data.Points[0].MarketCapUSD != nil || result.Data.Asset.AnchorPrice != nil {
		t.Fatalf("invalid optional values: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"market_cap_usd":null`) || strings.Contains(response.Body.String(), "raw_values") {
		t.Fatal("snapshot response lost null/provenance boundaries")
	}
	for _, bad := range []string{path + "&limit=0", path + "&cursor=bad", "/api/v1/market-snapshots?asset_id=coingecko.usd-coin&from=2000-01-01T00:00:00Z&to=2023-01-02T00:00:00Z"} {
		status(t, request(t, f.handler, "GET", bad, tokens.AccessToken, nil), 400)
	}
	response = request(t, f.handler, "GET", "/api/v1/trends?asset_id=coingecko.usd-coin&from=2023-01-01T00:00:00Z&to=2023-01-02T00:00:00Z&interval=1h", tokens.AccessToken, nil)
	status(t, response, 200)
	if !strings.Contains(response.Body.String(), `"volume":null`) || !strings.Contains(response.Body.String(), `"source_time":"2023-01-01T01:00:00Z"`) {
		t.Fatalf("hourly semantics lost: %s", response.Body.String())
	}
}
