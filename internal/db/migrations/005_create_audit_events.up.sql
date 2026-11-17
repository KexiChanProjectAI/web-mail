-- Audit trail of sign-ins, mailbox access and admin changes
-- Up migration: 005_create_audit_events
-- No foreign keys: events must outlive the users and messages they mention

CREATE TABLE IF NOT EXISTS audit_events (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    occurred_at DATETIME(3) NOT NULL COMMENT 'UTC',
    actor_email VARCHAR(320) NOT NULL COMMENT 'Login email, admin for the emergency key, empty when unknown',
    provider VARCHAR(32) NOT NULL DEFAULT '' COMMENT 'cf_access, google, admin_psk',
    session_key VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'Groups the events of one sign-in',
    event VARCHAR(40) NOT NULL,
    mailbox VARCHAR(500) NULL COMMENT 'Recipient mailbox the access went through',
    message_id BIGINT NULL,
    detail VARCHAR(2000) NULL,
    client_ip VARCHAR(64) NULL,
    user_agent VARCHAR(500) NULL,
    request_id VARCHAR(64) NULL,
    INDEX idx_audit_time (occurred_at),
    INDEX idx_audit_actor_time (actor_email, occurred_at),
    INDEX idx_audit_mailbox_time (mailbox, occurred_at),
    INDEX idx_audit_event_time (event, occurred_at),
    INDEX idx_audit_session (actor_email, session_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
