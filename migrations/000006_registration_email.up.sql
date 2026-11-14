-- Existing accounts have a NULL email and can still log in.
ALTER TABLE users ADD COLUMN email VARCHAR(254) CHARACTER SET ascii COLLATE ascii_bin NULL UNIQUE;
ALTER TABLE users ADD COLUMN email_verified_at DATETIME(3) NULL;

CREATE TABLE IF NOT EXISTS auth_registration_codes (
 email VARCHAR(254) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
 code_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 expires_at DATETIME(3) NOT NULL,
 resend_at DATETIME(3) NOT NULL,
 attempts_left INT NOT NULL,
 KEY idx_registration_codes_expiry (expires_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS auth_registration_sends (
 source_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
 expires_at DATETIME(3) NOT NULL,
 sent INT NOT NULL,
 KEY idx_registration_sends_expiry (expires_at)
) ENGINE=InnoDB;
