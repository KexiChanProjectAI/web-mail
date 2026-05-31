package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrDeliveryNotFound = errors.New("delivery record not found")

func CreateOrUpdateDelivery(ctx context.Context, db *sql.DB, messageID int64, status string, lastError string, attemptedAt, sentAt *time.Time) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO telegram_deliveries (message_id, status, last_error, attempted_at, sent_at)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			status = VALUES(status),
			last_error = VALUES(last_error),
			attempted_at = VALUES(attempted_at),
			sent_at = VALUES(sent_at)
	`, messageID, status, lastError, attemptedAt, sentAt)
	if err != nil {
		return fmt.Errorf("upsert telegram delivery: %w", err)
	}
	return nil
}

func GetDeliveryStatus(ctx context.Context, db *sql.DB, messageID int64) (*TelegramDelivery, error) {
	var d TelegramDelivery
	var lastError sql.NullString
	var attemptedAt, sentAt sql.NullTime

	err := db.QueryRowContext(ctx, `
		SELECT id, message_id, status, last_error, attempted_at, sent_at, created_at, updated_at
		FROM telegram_deliveries
		WHERE message_id = ?
	`, messageID).Scan(
		&d.ID, &d.MessageID, &d.Status,
		&lastError, &attemptedAt, &sentAt,
		&d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDeliveryNotFound
		}
		return nil, fmt.Errorf("query telegram delivery: %w", err)
	}

	if lastError.Valid {
		d.LastError = lastError.String
	}
	if attemptedAt.Valid {
		d.AttemptedAt = &attemptedAt.Time
	}
	if sentAt.Valid {
		d.SentAt = &sentAt.Time
	}

	return &d, nil
}
