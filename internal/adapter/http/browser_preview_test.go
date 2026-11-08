package httpserver_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"depeg.local/platform/internal/adapter/dataset/coingecko"
	"depeg.local/platform/internal/adapter/persistence/mysql"
	"depeg.local/platform/internal/domain/dataset"
	"depeg.local/platform/internal/port"
)

// Opt-in browser fixture. It never reads the application .env or connects to MySQL.
// Supply a downloaded archive, a stop-file path, and run this test with -v.
func TestBrowserPreview(t *testing.T) {
	archive := os.Getenv("DEPEG_BROWSER_DATASET")
	stop := os.Getenv("DEPEG_BROWSER_STOP")
	if archive == "" || stop == "" {
		t.Skip("browser fixture not requested")
	}
	f := setup(t)
	ctx := context.Background()
	reader := coingecko.New()
	if err := mysql.NewDatasetRepository(f.database).Import(ctx, func(sink port.DatasetSink) error {
		_, err := reader.Scan(ctx, archive, dataset.Options{Kind: "daily", CoinIDs: []string{"tether", "usd-coin", "dai", "terrausd"}}, func(batch dataset.Batch) error { return sink.Write(ctx, batch) })
		return err
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(f.handler)
	defer server.Close()
	fmt.Println("BROWSER_PREVIEW_URL=" + server.URL)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(15 * time.Minute)
	defer deadline.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(stop); err == nil {
				return
			}
		case <-deadline.C:
			return
		}
	}
}
