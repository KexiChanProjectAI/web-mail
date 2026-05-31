package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

var ErrShareTokenNotFound = errors.New("share token not found")

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func CreateShareToken(ctx context.Context, db *sql.DB, messageID int64) (string, error) {
	var existingToken string
	err := db.QueryRowContext(ctx,
		"SELECT token FROM share_tokens WHERE message_id = ?", messageID,
	).Scan(&existingToken)
	if err == nil {
		return existingToken, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("query existing share token: %w", err)
	}

	token, err := generateToken()
	if err != nil {
		return "", err
	}

	_, err = db.ExecContext(ctx,
		"INSERT INTO share_tokens (message_id, token) VALUES (?, ?)",
		messageID, token,
	)
	if err != nil {
		return "", fmt.Errorf("insert share token: %w", err)
	}

	return token, nil
}

func FindMessageIDByToken(ctx context.Context, db *sql.DB, token string) (int64, error) {
	var messageID int64
	err := db.QueryRowContext(ctx,
		"SELECT message_id FROM share_tokens WHERE token = ?", token,
	).Scan(&messageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrShareTokenNotFound
		}
		return 0, fmt.Errorf("query share token: %w", err)
	}
	return messageID, nil
}

func GetShareTokenByMessageID(ctx context.Context, db *sql.DB, messageID int64) (string, error) {
	var token string
	err := db.QueryRowContext(ctx,
		"SELECT token FROM share_tokens WHERE message_id = ?", messageID,
	).Scan(&token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrShareTokenNotFound
		}
		return "", fmt.Errorf("query share token by message: %w", err)
	}
	return token, nil
}
