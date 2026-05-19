-- Create mailbox tables for lite-mail
-- Up migration: 001_create_mailbox_tables

-- Messages table: stores parsed email metadata and raw MIME path reference
CREATE TABLE IF NOT EXISTS messages (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cloudflare_message_id VARCHAR(255) NOT NULL,
    content_hash VARCHAR(64) NOT NULL COMMENT 'SHA-256 hash for deduplication',
    sender VARCHAR(500) NOT NULL COMMENT 'Canonicalized sender email',
    subject VARCHAR(1000),
    message_date DATETIME NOT NULL COMMENT 'Date from email header',
    received_at DATETIME NOT NULL COMMENT 'When we received the message',
    text_body LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci,
    html_body LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci,
    raw_mime_path VARCHAR(1000) NOT NULL COMMENT 'Path to raw MIME in DATA_DIR/raw/',
    parser_status ENUM('success', 'partial', 'failed') NOT NULL DEFAULT 'success',
    parser_error TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_messages_content_hash (content_hash),
    INDEX idx_messages_sender (sender),
    INDEX idx_messages_received_at (received_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Message recipients table: normalized recipient list
CREATE TABLE IF NOT EXISTS message_recipients (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    message_id BIGINT NOT NULL,
    recipient_email VARCHAR(500) NOT NULL COMMENT 'Canonicalized recipient email',
    recipient_type ENUM('to', 'cc', 'bcc') NOT NULL,
    received_at DATETIME NOT NULL COMMENT 'When the message was received (copied from messages.received_at)',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE,
    INDEX idx_recipients_email_received (recipient_email, received_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Attachments table: metadata only, files stored on filesystem
CREATE TABLE IF NOT EXISTS attachments (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    message_id BIGINT NOT NULL,
    storage_key VARCHAR(500) NOT NULL COMMENT 'Safe generated path in DATA_DIR/attachments/',
    original_filename VARCHAR(500) COMMENT 'Original filename metadata only, not used in path',
    mime_type VARCHAR(255) NOT NULL,
    size_bytes BIGINT NOT NULL,
    content_hash VARCHAR(64) NOT NULL COMMENT 'SHA-256 hash for integrity',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE,
    INDEX idx_attachments_message_id (message_id),
    UNIQUE INDEX idx_attachments_storage_key (storage_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Ingest events table: dedupe tracking via cloudflare_message_id + content_hash
CREATE TABLE IF NOT EXISTS ingest_events (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    cloudflare_message_id VARCHAR(255) NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    status ENUM('accepted', 'duplicate', 'rejected') NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE INDEX idx_ingest_dedupe (cloudflare_message_id, content_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Fulltext index for search: sender + subject + text_body
-- Applied separately for MariaDB compatibility
ALTER TABLE messages ADD FULLTEXT INDEX ft_messages_search (sender, subject, text_body);
