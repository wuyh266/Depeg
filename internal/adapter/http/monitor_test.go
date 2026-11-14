package httpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"depeg.local/platform/internal/adapter/persistence/mysql"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/dataset"
	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/domain/monitor"
	"depeg.local/platform/internal/port"
)

func monitorData[T any](t *testing.T, r *httptest.ResponseRecorder) T {
	t.Helper()
	status(t, r, 200)
	var body struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data
}

func seedMonitor(t *testing.T, f *fixture) {
	t.Helper()
	a := market.Asset{ID: "coingecko.test-usd", Symbol: "TEST", QuoteCurrency: "USD"}
	start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	points := []market.Snapshot{}
	for i, p := range []string{"1", "0.995", "1.01"} {
		points = append(points, market.Snapshot{AssetID: a.ID, Source: "coingecko:market-chart", Time: start.Add(time.Duration(i) * 24 * time.Hour), PriceUSD: p, QualityFlags: []string{}, RawValues: "{}"})
	}
	if err := mysql.NewDatasetRepository(f.database).Import(context.Background(), func(sink port.DatasetSink) error {
		return sink.Write(context.Background(), dataset.Batch{Asset: &a, Snapshots: points})
	}); err != nil {
		t.Fatal(err)
	}
	anchor := "1"
	hour := market.Asset{ID: "test-hourly", Symbol: "TEST", QuoteCurrency: "USD", AnchorPrice: &anchor}
	end := start.Add(3 * time.Hour)
	if err := f.repository.Import(context.Background(), hour, []market.Candle{{AssetID: hour.ID, Source: "paper-csv:okx", Time: end.Add(-time.Hour), SourceTime: &end, Interval: time.Hour, Open: "1", High: "1", Low: "0.99", Close: "0.99", Quality: "historical"}}); err != nil {
		t.Fatal(err)
	}
}

func TestDashboardRulesReplayAndAccountIsolation(t *testing.T) {
	f := setup(t)
	seedMonitor(t, f)
	tokens := login(t, f)
	for _, path := range []string{"/api/v1/dashboard", "/api/v1/monitor/events"} {
		status(t, request(t, f.handler, "GET", path, "", nil), 401)
	}
	status(t, request(t, f.handler, "POST", "/api/v1/monitor/rules", "", map[string]any{}), 401)
	overview := monitorData[monitor.Overview](t, request(t, f.handler, "GET", "/api/v1/dashboard", tokens.AccessToken, nil))
	if len(overview.Assets) != 2 {
		t.Fatalf("catalog=%+v", overview)
	}
	var daily, hourly monitor.Asset
	for _, a := range overview.Assets {
		if a.Asset.ID == "coingecko.test-usd" {
			daily = a
		} else {
			hourly = a
		}
	}
	if daily.Status != "unknown" || daily.AnchorPrice != nil || daily.Selected == nil || daily.Selected.Count != 3 || daily.Selected.Latest.Price == "" || daily.Freshness != "historical" {
		t.Fatalf("unknown anchor/catalog lost: %+v", daily)
	}
	if hourly.Status != "critical" || hourly.Selected.Latest.ObservedAt.Hour() != 3 || hourly.Selected.Latest.Volume24hUSD != nil {
		t.Fatalf("hourly provenance lost: %+v", hourly)
	}
	body := map[string]any{"asset_id": "coingecko.test-usd", "kind": "daily", "source": "coingecko:market-chart", "interval_seconds": 86400, "anchor_price": "1.00", "warning_bps": 50, "critical_bps": 100, "user_id": "untrusted"}
	rule := monitorData[monitor.Rule](t, request(t, f.handler, "POST", "/api/v1/monitor/rules", tokens.AccessToken, body))
	if len(rule.Version) != 64 {
		t.Fatal("rule identity missing")
	}
	body["anchor_price"] = "1"
	same := monitorData[monitor.Rule](t, request(t, f.handler, "POST", "/api/v1/monitor/rules", tokens.AccessToken, body))
	if same.Version != rule.Version {
		t.Fatal("equivalent anchors changed version")
	}
	query := map[string]any{"asset_id": rule.AssetID, "from": "2023-01-01T00:00:00Z", "to": "2023-01-04T00:00:00Z", "rule_version": rule.Version}
	for i := 0; i < 2; i++ {
		r := monitorData[monitor.ReplayResult](t, request(t, f.handler, "POST", "/api/v1/monitor/replay", tokens.AccessToken, query))
		want := int64(2)
		if i == 1 {
			want = 0
		}
		if r.Scanned != 3 || r.Matched != 2 || r.Inserted != want {
			t.Fatalf("replay %d: %+v", i, r)
		}
	}
	page := monitorData[monitor.EventPage](t, request(t, f.handler, "GET", "/api/v1/monitor/events?limit=1", tokens.AccessToken, nil))
	if len(page.Events) != 1 || page.NextCursor == "" || page.Events[0].Severity != "critical" || page.Events[0].RuleVersion != rule.Version {
		t.Fatalf("events %+v", page)
	}
	status(t, request(t, f.handler, "GET", "/api/v1/monitor/events?asset_id=other&cursor="+page.NextCursor, tokens.AccessToken, nil), 400)
	page2 := monitorData[monitor.EventPage](t, request(t, f.handler, "GET", "/api/v1/monitor/events?limit=1&cursor="+page.NextCursor, tokens.AccessToken, nil))
	if len(page2.Events) != 1 || page2.NextCursor != "" || page2.Events[0].Severity != "warning" || page2.Events[0].Direction != "below" {
		t.Fatalf("second page %+v", page2)
	}
	credentials := map[string]string{"username": "another-user", "password": "another-password"}
	status(t, registerAccount(t, f, credentials), 201)
	other := pair(t, request(t, f.handler, "POST", "/api/v1/auth/login", "", credentials))
	otherOverview := monitorData[monitor.Overview](t, request(t, f.handler, "GET", "/api/v1/dashboard", other.AccessToken, nil))
	for _, a := range otherOverview.Assets {
		if a.Rule != nil {
			t.Fatal("another account saw a private rule")
		}
	}
	otherPage := monitorData[monitor.EventPage](t, request(t, f.handler, "GET", "/api/v1/monitor/events", other.AccessToken, nil))
	if len(otherPage.Events) != 0 {
		t.Fatal("event ownership leak")
	}
	status(t, request(t, f.handler, "POST", "/api/v1/monitor/replay", other.AccessToken, query), 404)
	status(t, request(t, f.handler, "GET", "/api/v1/monitor/events?cursor="+page.NextCursor, other.AccessToken, nil), 400)
	body["warning_bps"] = 200
	body["critical_bps"] = 300
	changed := monitorData[monitor.Rule](t, request(t, f.handler, "POST", "/api/v1/monitor/rules", tokens.AccessToken, body))
	if changed.Version == rule.Version {
		t.Fatal("changed thresholds retained version")
	}
	status(t, request(t, f.handler, "POST", "/api/v1/monitor/replay", tokens.AccessToken, query), 409)
	query["rule_version"] = changed.Version
	r := monitorData[monitor.ReplayResult](t, request(t, f.handler, "POST", "/api/v1/monitor/replay", tokens.AccessToken, query))
	if r.Matched != 0 || r.Inserted != 0 {
		t.Fatalf("normal prices created events: %+v", r)
	}
	old := monitorData[monitor.EventPage](t, request(t, f.handler, "GET", "/api/v1/monitor/events", tokens.AccessToken, nil))
	if len(old.Events) != 2 {
		t.Fatal("rule change erased old observations")
	}
	for _, bad := range []map[string]any{
		{"asset_id": rule.AssetID, "kind": "daily", "source": "coingecko:market-chart", "interval_seconds": 86400, "anchor_price": "0", "warning_bps": 50, "critical_bps": 100},
		{"asset_id": rule.AssetID, "kind": "daily", "source": "nonexistent", "interval_seconds": 86400, "anchor_price": "1", "warning_bps": 50, "critical_bps": 100},
		{"asset_id": rule.AssetID, "kind": "daily", "source": "coingecko:market-chart", "interval_seconds": 86400, "anchor_price": "1", "warning_bps": 100, "critical_bps": 50},
	} {
		status(t, request(t, f.handler, "POST", "/api/v1/monitor/rules", tokens.AccessToken, bad), 400)
	}
	status(t, request(t, f.handler, "GET", "/api/v1/monitor/events?limit=101", tokens.AccessToken, nil), 400)
}

func TestDashboardRefreshReadsCommittedImport(t *testing.T) {
	f := setup(t)
	seedMonitor(t, f)
	token := login(t, f).AccessToken
	readStream := func() monitor.Stream {
		t.Helper()
		overview := monitorData[monitor.Overview](t, request(t, f.handler, "GET", "/api/v1/dashboard", token, nil))
		for _, asset := range overview.Assets {
			if asset.Asset.ID == "coingecko.test-usd" && asset.Selected != nil {
				return *asset.Selected
			}
		}
		t.Fatal("daily stream missing from dashboard")
		return monitor.Stream{}
	}
	initial := readStream()
	snapshot := market.Snapshot{AssetID: "coingecko.test-usd", Source: initial.Source,
		Time: initial.LastAt.Add(24 * time.Hour), PriceUSD: "0.98", RawValues: "{}"}
	batch := dataset.Batch{Snapshots: []market.Snapshot{snapshot, snapshot}}
	ctx := context.Background()
	repo := mysql.NewDatasetRepository(f.database)
	if err := repo.Import(ctx, func(sink port.DatasetSink) error {
		if err := sink.Write(ctx, batch); err != nil {
			return err
		}
		return domain.ErrConflict
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected import rollback, got %v", err)
	}
	if rolledBack := readStream(); rolledBack.Count != initial.Count || !rolledBack.LastAt.Equal(initial.LastAt) {
		t.Fatalf("dashboard observed rolled back import: %+v", rolledBack)
	}
	for range 2 {
		if err := repo.Import(ctx, func(sink port.DatasetSink) error { return sink.Write(ctx, batch) }); err != nil {
			t.Fatal(err)
		}
		got := readStream()
		if got.Count != initial.Count+1 || !got.LastAt.Equal(snapshot.Time) || got.Latest.Price != snapshot.PriceUSD {
			t.Fatalf("dashboard did not reflect committed import: %+v", got)
		}
	}
}

func TestEmbeddedWebAndSameOriginLogin(t *testing.T) {
	f := setup(t)
	for path, content := range map[string]string{"/": "稳定币", "/static/app.js": "renderChart", "/static/charts.js": "sampleExtrema", "/static/app.css": "color-scheme"} {
		r := request(t, f.handler, "GET", path, "", nil)
		status(t, r, 200)
		if !strings.Contains(r.Body.String(), content) || r.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing embedded app or policy", path)
		}
	}
	status(t, request(t, f.handler, "GET", "/static/README.md", "", nil), 404)
	email := "same-origin@example.test"
	code := sendRegistrationCode(t, f, email)
	body := strings.NewReader(fmt.Sprintf(`{"username":"same-origin","password":"a-strong-password","email":%q,"email_code":%q}`, email, code))
	req := httptest.NewRequest("POST", "http://example.com/api/v1/auth/register", body)
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Content-Type", "application/json")
	r := httptest.NewRecorder()
	f.handler.ServeHTTP(r, req)
	status(t, r, 201)
	req = httptest.NewRequest("POST", "http://example.com/api/v1/auth/register", strings.NewReader(`{}`))
	req.Header.Set("Origin", "http://untrusted.test")
	req.Header.Set("Content-Type", "application/json")
	r = httptest.NewRecorder()
	f.handler.ServeHTTP(r, req)
	status(t, r, 403)
}
