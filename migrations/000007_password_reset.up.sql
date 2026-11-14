CREATE TABLE IF NOT EXISTS auth_password_reset_codes (
 email VARCHAR(254) CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
 code_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 expires_at DATETIME(3) NOT NULL,
 resend_at DATETIME(3) NOT NULL,
 attempts_left INT NOT NULL,
 KEY idx_password_reset_codes_expiry (expires_at)
) ENGINE=InnoDB;
