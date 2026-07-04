-- Recreate share_tokens and telegram_deliveries.
--
-- This is the symmetric inverse of 003_drop_share_and_delivery_tables.up.sql.
-- The shapes below match 002_add_share_and_delivery_tables.up.sql verbatim
-- so that `migrate down` followed by `migrate up` leaves the database in
-- the same state as before this migration.
--
-- Share tokens table: one permanent token per message
CREATE TABLE IF NOT EXISTS share_tokens (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    message_id BIGINT NOT NULL,
    token VARCHAR(128) NOT NULL COMMENT 'crypto-random 64-char hex token (32 bytes)',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE INDEX idx_share_tokens_token (token),
    INDEX idx_share_tokens_message_id (message_id),
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Telegram delivery status table: one record per message attempt
CREATE TABLE IF NOT EXISTS telegram_deliveries (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    message_id BIGINT NOT NULL,
    status ENUM('pending','skipped','sent','failed') NOT NULL DEFAULT 'pending',
    last_error TEXT,
    attempted_at DATETIME,
    sent_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE INDEX idx_telegram_deliveries_message_id (message_id),
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
