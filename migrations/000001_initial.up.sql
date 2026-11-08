-- Explicit MySQL 8.0+ schema. Run cmd/migrate; the API never migrates automatically.
CREATE TABLE IF NOT EXISTS users (
 id VARCHAR(32) NOT NULL PRIMARY KEY,
 username VARCHAR(32) NOT NULL,
 password_hash VARCHAR(100) NOT NULL,
 disabled BOOLEAN NOT NULL DEFAULT FALSE,
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 UNIQUE KEY uq_users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS auth_sessions (
 id VARCHAR(32) NOT NULL PRIMARY KEY,
 user_id VARCHAR(32) NOT NULL,
 refresh_hash VARCHAR(64) NOT NULL,
 expires_at DATETIME(3) NOT NULL,
 revoked_at DATETIME(3) NULL,
 created_at DATETIME(3) NOT NULL,
 UNIQUE KEY uq_auth_sessions_hash (refresh_hash),
 KEY idx_sessions_user (user_id),
 KEY idx_sessions_expiry (expires_at),
 CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS assets (
 id VARCHAR(96) NOT NULL PRIMARY KEY,
 symbol VARCHAR(32) NOT NULL,
 chain VARCHAR(64) NOT NULL,
 token_address VARCHAR(128) NOT NULL,
 quote_currency VARCHAR(16) NOT NULL,
 anchor_price DECIMAL(38,18) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS candles (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 asset_id VARCHAR(96) NOT NULL,
 source VARCHAR(64) NOT NULL,
 interval_seconds BIGINT NOT NULL,
 open_time DATETIME(3) NOT NULL,
 `open` DECIMAL(38,18) NOT NULL,
 high DECIMAL(38,18) NOT NULL,
 low DECIMAL(38,18) NOT NULL,
 `close` DECIMAL(38,18) NOT NULL,
 volume DECIMAL(38,18) NOT NULL,
 quality VARCHAR(32) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 UNIQUE KEY uq_candles (asset_id,source,interval_seconds,open_time),
 KEY idx_candle_range (asset_id,interval_seconds,open_time,source),
 CONSTRAINT fk_candles_asset FOREIGN KEY (asset_id) REFERENCES assets(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
