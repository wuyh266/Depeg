CREATE TABLE IF NOT EXISTS market_stream_summaries (
 asset_id VARCHAR(96) NOT NULL,
 kind VARCHAR(8) NOT NULL,
 source VARCHAR(64) NOT NULL,
 interval_seconds BIGINT NOT NULL,
 first_at DATETIME(3) NOT NULL,
 last_at DATETIME(3) NOT NULL,
 sample_count BIGINT NOT NULL,
 PRIMARY KEY (asset_id, kind, source, interval_seconds),
 CONSTRAINT fk_stream_summary_asset FOREIGN KEY (asset_id) REFERENCES assets(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Replace counts on retry so a partially applied migration never counts data twice.
INSERT INTO market_stream_summaries
 (asset_id, kind, source, interval_seconds, first_at, last_at, sample_count)
SELECT * FROM (
 SELECT asset_id, 'ohlc' AS kind, source, interval_seconds,
        MIN(open_time) AS first_at, MAX(open_time) AS last_at, COUNT(*) AS sample_count
 FROM candles GROUP BY asset_id, source, interval_seconds
) AS history
ON DUPLICATE KEY UPDATE first_at = history.first_at,
 last_at = history.last_at, sample_count = history.sample_count;

INSERT INTO market_stream_summaries
 (asset_id, kind, source, interval_seconds, first_at, last_at, sample_count)
SELECT * FROM (
 SELECT asset_id, 'daily' AS kind, source, 86400 AS interval_seconds,
        MIN(observed_at) AS first_at, MAX(observed_at) AS last_at, COUNT(*) AS sample_count
 FROM market_snapshots GROUP BY asset_id, source
) AS history
ON DUPLICATE KEY UPDATE first_at = history.first_at,
 last_at = history.last_at, sample_count = history.sample_count;
