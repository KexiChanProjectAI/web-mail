-- Drop the share_tokens and telegram_deliveries tables.
--
-- These tables were created by migration 002 to support Go-side
-- public share-link rendering (/share/{token}) and Telegram delivery
-- status tracking. Both responsibilities have been retired in favor
-- of the Cloudflare Worker, which now hosts:
--   * /email/:id?mode=text|html   (preview surfaces, replacement for /share/{token})
--   * Telegram delivery (no Go-side audit row needed)
--
-- Safe to drop: no production code reads or writes these tables after
-- task 5 (ingest) and task 6 (share routes / share handler / db helpers)
-- of the worker-owns-telegram-delivery plan.
DROP TABLE IF EXISTS telegram_deliveries;
DROP TABLE IF EXISTS share_tokens;
