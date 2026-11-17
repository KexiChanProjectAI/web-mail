-- Envelope recipient: the address Cloudflare Email Routing delivered to (RCPT TO)
-- Up migration: 005_add_envelope_recipient_type
-- Mailbox grants match envelope rows, To/Cc/Bcc rows are kept for display

ALTER TABLE message_recipients MODIFY recipient_type ENUM('to', 'cc', 'bcc', 'envelope') NOT NULL;
