CREATE TABLE IF NOT EXISTS data_versions (
 name VARCHAR(64) NOT NULL PRIMARY KEY,
 version BIGINT UNSIGNED NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO data_versions (name, version)
VALUES ('assets', 1)
ON DUPLICATE KEY UPDATE name = 'assets';