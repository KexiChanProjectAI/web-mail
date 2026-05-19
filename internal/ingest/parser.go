package ingest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"time"
)

// ParsedMessage contains metadata and content extracted from a raw MIME message.
type ParsedMessage struct {
	Sender      string
	Subject     string
	MessageDate time.Time
	Recipients  []Recipient
	TextBody    string
	HTMLBody    string
	Attachments []ParsedAttachment
	ContentHash string
}

// ParsedAttachment contains an attachment payload and metadata.
type ParsedAttachment struct {
	Content          []byte
	MimeType         string
	OriginalFilename string
	Size             int64
	ContentHash      string
}

// Recipient is a canonicalized message recipient.
type Recipient struct {
	Email string
	Type  string
}

// ParseMIME parses a raw RFC 822/MIME message using Go standard library parsers.
func ParseMIME(raw []byte) (*ParsedMessage, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("read MIME message: %w", err)
	}

	parsed := &ParsedMessage{ContentHash: HashBytes(raw)}
	if from := msg.Header.Get("From"); from != "" {
		addr, err := mail.ParseAddress(from)
		if err != nil {
			return nil, fmt.Errorf("parse From header: %w", err)
		}
		parsed.Sender = canonicalEmail(addr.Address)
	}

	parsed.Subject, err = (&mime.WordDecoder{}).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		parsed.Subject = msg.Header.Get("Subject")
	}
	if date, err := msg.Header.Date(); err == nil {
		parsed.MessageDate = date
	}
	parsed.Recipients = append(parsed.Recipients, parseRecipients(msg.Header, "To", "to")...)
	parsed.Recipients = append(parsed.Recipients, parseRecipients(msg.Header, "Cc", "cc")...)
	parsed.Recipients = append(parsed.Recipients, parseRecipients(msg.Header, "Bcc", "bcc")...)

	if err := parseEntity(msg.Header, msg.Body, parsed); err != nil {
		return parsed, err
	}
	return parsed, nil
}

// HashBytes returns the SHA-256 hash of data as lowercase hexadecimal.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func parseRecipients(header mail.Header, name, recipientType string) []Recipient {
	values := header[name]
	recipients := make([]Recipient, 0)
	for _, value := range values {
		addresses, err := mail.ParseAddressList(value)
		if err != nil {
			continue
		}
		for _, addr := range addresses {
			if email := canonicalEmail(addr.Address); email != "" {
				recipients = append(recipients, Recipient{Email: email, Type: recipientType})
			}
		}
	}
	return recipients
}

func parseEntity(header mail.Header, body io.Reader, parsed *ParsedMessage) error {
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	mediaType = strings.ToLower(mediaType)

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return fmt.Errorf("multipart message missing boundary")
		}
		reader := multipart.NewReader(body, boundary)
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("read MIME part: %w", err)
			}
			partHeader := mail.Header(part.Header)
			if err := parsePart(partHeader, part, parsed); err != nil {
				return err
			}
		}
		return nil
	}

	return consumeLeaf(header, body, mediaType, parsed)
}

func parsePart(header mail.Header, body io.Reader, parsed *ParsedMessage) error {
	mediaType, _, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	mediaType = strings.ToLower(mediaType)
	if strings.HasPrefix(mediaType, "multipart/") {
		return parseEntity(header, body, parsed)
	}
	return consumeLeaf(header, body, mediaType, parsed)
}

func consumeLeaf(header mail.Header, body io.Reader, mediaType string, parsed *ParsedMessage) error {
	content, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("read MIME leaf: %w", err)
	}
	_, dispositionParams, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	_, contentTypeParams, _ := mime.ParseMediaType(header.Get("Content-Type"))
	filename := dispositionParams["filename"]
	if filename == "" {
		filename = contentTypeParams["name"]
	}
	disposition := strings.ToLower(header.Get("Content-Disposition"))
	isAttachment := filename != "" || strings.HasPrefix(disposition, "attachment")

	if isAttachment {
		parsed.Attachments = append(parsed.Attachments, ParsedAttachment{
			Content:          content,
			MimeType:         mediaType,
			OriginalFilename: filename,
			Size:             int64(len(content)),
			ContentHash:      HashBytes(content),
		})
		return nil
	}

	switch mediaType {
	case "text/plain":
		parsed.TextBody += string(content)
	case "text/html":
		parsed.HTMLBody += string(content)
	}
	return nil
}

func canonicalEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
