package ingest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"lite-mail/internal/storage"
)

// BackfillResult counts what BackfillEnvelopeRecipients did.
type BackfillResult struct {
	Scanned    int
	Added      int
	NoEnvelope []int64 // message ids whose raw MIME has no Cloudflare Received header
	ReadErrors []int64 // message ids whose raw MIME could not be read
}

// BackfillEnvelopeRecipients adds the envelope recipient to messages stored
// before it was recorded, reading it from each message's raw MIME. With dryRun
// it only reports. The envelope recipient is shown as "Delivered to" and
// decides access for messages without To/Cc/Bcc recipients.
func BackfillEnvelopeRecipients(ctx context.Context, db *sql.DB, store *storage.Storage, dryRun bool) (*BackfillResult, error) {
	rows, err := db.QueryContext(ctx, `SELECT m.id, m.raw_mime_path, m.received_at FROM messages m
WHERE NOT EXISTS (SELECT 1 FROM message_recipients r WHERE r.message_id = m.id AND r.recipient_type = ?)
ORDER BY m.id`, RecipientTypeEnvelope)
	if err != nil {
		return nil, err
	}
	type pending struct {
		id         int64
		rawPath    string
		receivedAt sql.NullTime
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.rawPath, &p.receivedAt); err != nil {
			rows.Close()
			return nil, err
		}
		todo = append(todo, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	res := &BackfillResult{}
	for _, p := range todo {
		res.Scanned++
		raw, err := store.ReadRawMIME(strings.TrimPrefix(p.rawPath, "raw/"))
		if err != nil {
			res.ReadErrors = append(res.ReadErrors, p.id)
			continue
		}
		addr := EnvelopeRecipientFromRaw(raw)
		if addr == "" {
			res.NoEnvelope = append(res.NoEnvelope, p.id)
			continue
		}
		if !dryRun {
			if _, err := db.ExecContext(ctx, `INSERT INTO message_recipients (message_id, recipient_email, recipient_type, received_at) VALUES (?, ?, ?, ?)`,
				p.id, addr, RecipientTypeEnvelope, p.receivedAt); err != nil {
				return res, fmt.Errorf("message %d: %w", p.id, err)
			}
		}
		res.Added++
	}
	return res, nil
}
