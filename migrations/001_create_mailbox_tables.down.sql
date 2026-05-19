-- Rollback mailbox tables for lite-mail
-- Down migration: 001_create_mailbox_tables

ALTER TABLE messages DROP INDEX ft_messages_search;
DROP TABLE IF EXISTS ingest_events;
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS message_recipients;
DROP TABLE IF EXISTS messages;
