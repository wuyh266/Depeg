-- Account-scoped thresholds. Existing market data and anchors are unchanged.
CREATE TABLE IF NOT EXISTS monitor_rules (
 user_id VARCHAR(32) NOT NULL,
 asset_id VARCHAR(96) NOT NULL,
 kind VARCHAR(16) NOT NULL,
 source VARCHAR(64) NOT NULL,
 interval_seconds BIGINT NOT NULL,
 anchor_price DECIMAL(38,18) NOT NULL,
 warning_bps INT NOT NULL,
 critical_bps INT NOT NULL,
 version VARCHAR(64) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 PRIMARY KEY (user_id,asset_id),
 CONSTRAINT fk_monitor_rule_user FOREIGN KEY (user_id) REFERENCES users(id),
 CONSTRAINT fk_monitor_rule_asset FOREIGN KEY (asset_id) REFERENCES assets(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE IF NOT EXISTS monitor_events (
 id VARCHAR(64) NOT NULL PRIMARY KEY,
 user_id VARCHAR(32) NOT NULL,
 asset_id VARCHAR(96) NOT NULL,
 kind VARCHAR(16) NOT NULL,
 source VARCHAR(64) NOT NULL,
 interval_seconds BIGINT NOT NULL,
 time DATETIME(3) NOT NULL,
 observed_at DATETIME(3) NOT NULL,
 price DECIMAL(38,18) NOT NULL,
 anchor_price DECIMAL(38,18) NOT NULL,
 deviation_bps VARCHAR(96) NOT NULL,
 severity VARCHAR(16) NOT NULL,
 direction VARCHAR(16) NOT NULL,
 warning_bps INT NOT NULL,
 critical_bps INT NOT NULL,
 rule_version VARCHAR(64) NOT NULL,
 detected_at DATETIME(3) NOT NULL,
 KEY idx_monitor_event_user (user_id,observed_at,id),
 KEY idx_monitor_event_asset (user_id,asset_id,observed_at,id),
 CONSTRAINT fk_monitor_event_user FOREIGN KEY (user_id) REFERENCES users(id),
 CONSTRAINT fk_monitor_event_asset FOREIGN KEY (asset_id) REFERENCES assets(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
