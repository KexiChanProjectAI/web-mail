-- Down migration: 005_add_envelope_recipient_type
DELETE FROM message_recipients WHERE recipient_type = 'envelope';
ALTER TABLE message_recipients MODIFY recipient_type ENUM('to', 'cc', 'bcc') NOT NULL;
