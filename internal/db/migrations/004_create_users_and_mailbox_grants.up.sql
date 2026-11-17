-- Login identities and the mailboxes each one may read
-- Up migration: 004_create_users_and_mailbox_grants
-- Admin status from ADMIN_EMAILS is merged at request time and is not stored here

CREATE TABLE IF NOT EXISTS users (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    email VARCHAR(320) NOT NULL COMMENT 'Canonical lowercase login email',
    is_admin TINYINT(1) NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login_at DATETIME NULL,
    last_login_provider VARCHAR(32) NULL COMMENT 'google or cf_access',
    UNIQUE INDEX idx_users_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS mailbox_grants (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    pattern VARCHAR(500) NOT NULL COMMENT 'Exact recipient address or *@domain, lowercase',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by VARCHAR(320) NULL COMMENT 'Login email of the admin who added the grant',
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE INDEX idx_grants_user_pattern (user_id, pattern)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
