package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Storage persists raw MIME messages and attachment blobs under a configured data directory.
type Storage struct {
	baseDir string
}

// NewStorage creates the storage directory layout if it does not already exist.
func NewStorage(baseDir string) (*Storage, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, fmt.Errorf("storage base directory is required")
	}

	s := &Storage{baseDir: baseDir}
	if err := os.MkdirAll(filepath.Join(baseDir, "raw"), 0755); err != nil {
		return nil, fmt.Errorf("create raw storage directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "attachments"), 0755); err != nil {
		return nil, fmt.Errorf("create attachment storage directory: %w", err)
	}
	return s, nil
}

// SaveRawMIME writes a raw MIME blob by content hash. Existing files are left untouched.
func (s *Storage) SaveRawMIME(contentHash string, data []byte) error {
	if err := validateKey(contentHash); err != nil {
		return err
	}
	path := filepath.Join(s.baseDir, "raw", contentHash)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat raw MIME: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create raw MIME directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write raw MIME: %w", err)
	}
	return nil
}

// ReadRawMIME reads a raw MIME blob by content hash.
func (s *Storage) ReadRawMIME(contentHash string) ([]byte, error) {
	if err := validateKey(contentHash); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.baseDir, "raw", contentHash))
	if err != nil {
		return nil, fmt.Errorf("read raw MIME: %w", err)
	}
	return data, nil
}

// SaveAttachment writes an attachment blob under the message ID using a generated safe key.
func (s *Storage) SaveAttachment(messageID int64, safeKey string, data []byte) error {
	if messageID <= 0 {
		return fmt.Errorf("message ID must be positive")
	}
	if err := validateKey(safeKey); err != nil {
		return err
	}
	dir := filepath.Join(s.baseDir, "attachments", fmt.Sprintf("%d", messageID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create attachment directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, safeKey), data, 0644); err != nil {
		return fmt.Errorf("write attachment: %w", err)
	}
	return nil
}

// ReadAttachment reads an attachment blob for a message ID and generated safe key.
func (s *Storage) ReadAttachment(messageID int64, safeKey string) ([]byte, error) {
	if messageID <= 0 {
		return nil, fmt.Errorf("message ID must be positive")
	}
	if err := validateKey(safeKey); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.baseDir, "attachments", fmt.Sprintf("%d", messageID), safeKey))
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	return data, nil
}

func validateKey(key string) error {
	if key == "" || filepath.Base(key) != key || strings.ContainsAny(key, `/\`) {
		return fmt.Errorf("invalid storage key")
	}
	return nil
}
