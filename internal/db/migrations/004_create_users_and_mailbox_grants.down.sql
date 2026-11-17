-- Down migration: 004_create_users_and_mailbox_grants
DROP TABLE IF EXISTS mailbox_grants;
DROP TABLE IF EXISTS users;
