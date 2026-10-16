package ingest

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html/charset"
	"jaytaylor.com/html2text"
)

// headerDecoder decodes RFC 2047 encoded words in any charset known to the
// WHATWG encoding index, not only the utf-8/iso-8859-1/us-ascii built-ins.
var headerDecoder = &mime.WordDecoder{CharsetReader: charset.NewReaderLabel}

// addressParser parses address headers using headerDecoder for display names.
var addressParser = &mail.AddressParser{WordDecoder: headerDecoder}

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
		return &ParsedMessage{ContentHash: HashBytes(raw)}, fmt.Errorf("read MIME message: %w", err)
	}

	parsed := &ParsedMessage{ContentHash: HashBytes(raw)}
	if from := msg.Header.Get("From"); from != "" {
		addr, err := addressParser.Parse(from)
		if err != nil {
			parsed.Sender = decodeText([]byte(from), "") // Keep raw value on parse failure, as valid UTF-8
			// Don't return error - continue processing
		} else {
			parsed.Sender = canonicalEmail(addr.Address)
		}
	}

	parsed.Subject = decodeHeaderText(msg.Header.Get("Subject"))
	if date, err := msg.Header.Date(); err == nil {
		parsed.MessageDate = date
	}
	parsed.Recipients = append(parsed.Recipients, parseRecipients(msg.Header, "To", "to")...)
	parsed.Recipients = append(parsed.Recipients, parseRecipients(msg.Header, "Cc", "cc")...)
	parsed.Recipients = append(parsed.Recipients, parseRecipients(msg.Header, "Bcc", "bcc")...)

	if err := parseEntity(msg.Header, msg.Body, parsed); err != nil {
		return parsed, err
	}

	// If no text body was parsed and HTML body exists, convert HTML to text.
	if parsed.TextBody == "" && parsed.HTMLBody != "" {
		parsed.TextBody = htmlToText(parsed.HTMLBody)
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
		addresses, err := addressParser.ParseList(value)
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
	reader, err := decodeContent(header, body)
	if err != nil {
		return fmt.Errorf("decode MIME leaf: %w", err)
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read MIME leaf: %w", err)
	}
	_, dispositionParams, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	_, contentTypeParams, _ := mime.ParseMediaType(header.Get("Content-Type"))
	filename := dispositionParams["filename"]
	if filename == "" {
		filename = contentTypeParams["name"]
	}
	filename = decodeHeaderText(filename)
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
		parsed.TextBody += decodeText(content, contentTypeParams["charset"])
	case "text/html":
		parsed.HTMLBody += decodeText(content, contentTypeParams["charset"])
	}
	return nil
}

// decodeText converts a text/* MIME body to valid UTF-8 using the charset
// label from its Content-Type. A missing label means UTF-8. Valid UTF-8 that is
// labeled utf-8 or us-ascii is returned byte-for-byte. Unknown labels and bytes
// that are invalid for the declared UTF-8/ASCII label fall back to windows-1252
// (a superset of ISO-8859-1), so the result is always valid UTF-8 and the
// message is never dropped because of its encoding.
func decodeText(content []byte, charsetLabel string) string {
	label := strings.ToLower(strings.TrimSpace(charsetLabel))
	if label == "" {
		label = "utf-8"
	}

	switch label {
	case "utf-8", "utf8", "us-ascii", "ascii":
		if utf8.Valid(content) {
			return string(content)
		}
		// Mislabelled 8-bit content: treat as windows-1252.
		label = "windows-1252"
	}

	reader, err := charset.NewReaderLabel(label, bytes.NewReader(content))
	if err != nil {
		reader, err = charset.NewReaderLabel("windows-1252", bytes.NewReader(content))
	}
	if err == nil {
		if decoded, readErr := io.ReadAll(reader); readErr == nil {
			return strings.ToValidUTF8(string(decoded), "\uFFFD")
		}
	}
	return strings.ToValidUTF8(string(content), "\uFFFD")
}

// decodeHeaderText decodes RFC 2047 encoded words in a header value and
// guarantees valid UTF-8, falling back to the raw value when decoding fails.
func decodeHeaderText(value string) string {
	if value == "" {
		return ""
	}
	if decoded, err := headerDecoder.DecodeHeader(value); err == nil {
		value = decoded
	}
	return decodeText([]byte(value), "")
}

// decodeContent wraps the body reader with the appropriate decoder based on
// the Content-Transfer-Encoding header. Supported: quoted-printable, base64.
func decodeContent(header mail.Header, body io.Reader) (io.Reader, error) {
	encoding := strings.ToLower(header.Get("Content-Transfer-Encoding"))
	switch encoding {
	case "quoted-printable":
		return quotedprintable.NewReader(body), nil
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, body), nil
	case "":
		return body, nil
	default:
		// Unknown encoding: return body as-is rather than failing
		return body, nil
	}
}

// htmlToText converts HTML to plain text using the html2text library.
func htmlToText(s string) string {
	text, err := html2text.FromString(s, html2text.Options{TextOnly: true})
	if err != nil {
		return ""
	}
	return text
}

func canonicalEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
