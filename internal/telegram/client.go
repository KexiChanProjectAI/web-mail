package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Config holds Telegram client configuration.
type Config struct {
	BotToken string
	ChatID   string
	BaseURL  string // PUBLIC_BASE_URL for building share URLs
}

// Client sends messages via the Telegram Bot API.
type Client struct {
	cfg    Config
	http   *http.Client
	apiURL string
}

// NewClient creates a new Telegram client with the given configuration.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg: cfg,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
		apiURL: fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.BotToken),
	}
}

// Send posts a sendMessage request to the Telegram Bot API.
func (c *Client) Send(ctx context.Context, req *SendMessageRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram api error: status %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp SendMessageResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if !apiResp.OK {
		return fmt.Errorf("telegram api returned not ok: %s", apiResp.Description)
	}

	return nil
}
