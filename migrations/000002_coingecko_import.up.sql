-- Existing CSV data is retained. Unknown anchors and volume remain NULL.
ALTER TABLE assets MODIFY COLUMN anchor_price DECIMAL(38,18) NULL;
ALTER TABLE candles MODIFY COLUMN volume DECIMAL(38,18) NULL;
-- The runner checks existing columns so these additions can resume after a DDL failure.
ALTER TABLE candles ADD COLUMN source_time DATETIME(3) NULL;
ALTER TABLE candles ADD COLUMN raw_values LONGTEXT NULL;
CREATE TABLE IF NOT EXISTS market_snapshots (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 asset_id VARCHAR(96) NOT NULL,
 source VARCHAR(64) NOT NULL,
 observed_at DATETIME(3) NOT NULL,
 price_usd DECIMAL(38,18) NOT NULL,
 market_cap_usd DECIMAL(38,18) NULL,
 volume24h_usd DECIMAL(38,18) NULL,
 quality_flags LONGTEXT NOT NULL,
 raw_values LONGTEXT NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 UNIQUE KEY uq_snapshots (asset_id,source,observed_at),
 KEY idx_snapshot_range (asset_id,observed_at,source),
 CONSTRAINT fk_snapshots_asset FOREIGN KEY (asset_id) REFERENCES assets(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS news_articles (
 id VARCHAR(64) NOT NULL PRIMARY KEY,
 url TEXT NOT NULL,
 title TEXT NOT NULL,
 published_at DATETIME(3) NOT NULL,
 fetched_at DATETIME(3) NOT NULL,
 type VARCHAR(32) NOT NULL,
 source_name VARCHAR(256) NOT NULL,
 image_url TEXT NOT NULL,
 authors LONGTEXT NOT NULL,
 coin_ids LONGTEXT NOT NULL,
 raw_json LONGTEXT NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 KEY idx_news_time (published_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
