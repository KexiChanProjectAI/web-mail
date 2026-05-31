package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"lite-mail/internal/config"
	"lite-mail/internal/db"
)

// Sender interface for testability (allows mocking the HTTP client).
type Sender interface {
	Send(ctx context.Context, req *SendMessageRequest) error
}

// DeliveryService coordinates delivery-record creation/update around Telegram sends.
type DeliveryService struct {
	db     *sql.DB
	cfg    *config.Config
	sender Sender
}

// NewDeliveryService creates a new DeliveryService.
func NewDeliveryService(database *sql.DB, cfg *config.Config, sender Sender) *DeliveryService {
	return &DeliveryService{
		db:     database,
		cfg:    cfg,
		sender: sender,
	}
}

// Deliver sends a message to Telegram and records the delivery status.
// It is idempotent: if a delivery record already exists with "sent" status, it returns nil without sending.
// If Telegram is disabled in config, it records "skipped" status and returns nil.
func (s *DeliveryService) Deliver(ctx context.Context, messageID int64, summary string, markup *ReplyMarkup) error {
	// Check if Telegram is enabled
	if !s.cfg.TelegramEnabled() {
		now := time.Now()
		if err := db.CreateOrUpdateDelivery(ctx, s.db, messageID, "skipped", "", &now, nil); err != nil {
			return fmt.Errorf("record skipped delivery: %w", err)
		}
		return nil
	}

	// Check existing delivery status for idempotency
	existing, err := db.GetDeliveryStatus(ctx, s.db, messageID)
	if err != nil && !errors.Is(err, db.ErrDeliveryNotFound) {
		return fmt.Errorf("check delivery status: %w", err)
	}

	// If already sent, don't send again (idempotency)
	if existing != nil && existing.Status == "sent" {
		return nil
	}

	// Build the send request
	req := &SendMessageRequest{
		ChatID:      s.cfg.TelegramChatID,
		Text:        summary,
		ParseMode:   "HTML",
		ReplyMarkup: markup,
	}

	// Attempt to send
	now := time.Now()
	sendErr := s.sender.Send(ctx, req)

	if sendErr != nil {
		// Record failure
		if err := db.CreateOrUpdateDelivery(ctx, s.db, messageID, "failed", sendErr.Error(), &now, nil); err != nil {
			return fmt.Errorf("record failed delivery: %w (original error: %v)", err, sendErr)
		}
		return sendErr
	}

	// Record success
	sentAt := time.Now()
	if err := db.CreateOrUpdateDelivery(ctx, s.db, messageID, "sent", "", &now, &sentAt); err != nil {
		return fmt.Errorf("record sent delivery: %w", err)
	}

	return nil
}
