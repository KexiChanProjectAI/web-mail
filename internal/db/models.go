package db

import "time"

// ShareToken represents a permanent share token for a message.
// One token per message; calling CreateShareToken again returns the existing token.
type ShareToken struct {
	ID        int64
	MessageID int64
	Token     string
	CreatedAt time.Time
}

// TelegramDelivery represents the delivery status of a message to Telegram.
type TelegramDelivery struct {
	ID          int64
	MessageID   int64
	Status      string // pending, skipped, sent, failed
	LastError   string
	AttemptedAt *time.Time
	SentAt      *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
