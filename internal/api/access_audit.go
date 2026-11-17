package api

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strings"

	"lite-mail/internal/audit"
	"lite-mail/internal/auth"
	maildb "lite-mail/internal/db"
)

// maxAuditMailboxes caps the rows written for one access. A message reached
// through several granted mailboxes gets one row per mailbox, so filtering
// the log by mailbox finds it.
const maxAuditMailboxes = 5

// recordAccess logs that session opened messageID, once per recipient
// mailbox the session is allowed to read.
func recordAccess(l *audit.Logger, db *sql.DB, r *http.Request, session *auth.Session, event string, messageID int64, opts ...audit.Option) {
	if l == nil || session == nil {
		return
	}
	mailboxes, err := accessedMailboxes(r.Context(), db, session, messageID)
	if err != nil {
		slog.Warn("audit: recipient lookup failed", "message_id", messageID, "error", err.Error())
	}
	base := append([]audit.Option{audit.WithMessage(messageID)}, opts...)
	if len(mailboxes) == 0 {
		l.Record(r, session, event, base...)
		return
	}
	extra := len(mailboxes) - maxAuditMailboxes
	if extra > 0 {
		mailboxes = mailboxes[:maxAuditMailboxes]
	}
	for i, mailbox := range mailboxes {
		rowOpts := append(append([]audit.Option{}, base...), audit.WithMailbox(mailbox))
		if i == len(mailboxes)-1 && extra > 0 {
			rowOpts = append(rowOpts, audit.WithDetail("+%d more recipient mailboxes", extra))
		}
		l.Record(r, session, event, rowOpts...)
	}
}

// accessedMailboxes returns the message's mailbox recipients that the session
// may read, in order, without duplicates. Mailbox recipients are the envelope
// recipients, or To/Cc/Bcc when none was recorded, as in scopeClause.
func accessedMailboxes(ctx context.Context, db *sql.DB, session *auth.Session, messageID int64) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT mr.recipient_email FROM message_recipients mr WHERE mr.message_id = ? AND "+
		maildb.MailboxRecipientCondition("mr", "mr.message_id")+" ORDER BY mr.id", messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var address string
		if err := rows.Scan(&address); err != nil {
			return nil, err
		}
		address = strings.ToLower(address)
		if seen[address] || !session.HasMailboxAccess(address) {
			continue
		}
		seen[address] = true
		out = append(out, address)
	}
	return out, rows.Err()
}

// recordDenied logs an attempt to open an existing message outside the
// session's grants. A message id that does not exist is not logged.
func recordDenied(l *audit.Logger, db *sql.DB, r *http.Request, session *auth.Session, messageID int64, what string) {
	if l == nil || session == nil || session.IsAdmin {
		return
	}
	var exists int
	if err := db.QueryRowContext(r.Context(), "SELECT 1 FROM messages WHERE id = ?", messageID).Scan(&exists); err != nil {
		return
	}
	// The message exists; it is only a denial if none of its recipients is
	// granted (an out-of-range attachment index on a readable message is not).
	if granted, err := accessedMailboxes(r.Context(), db, session, messageID); err != nil || len(granted) > 0 {
		return
	}
	l.Record(r, session, audit.AccessDenied, audit.WithMessage(messageID), audit.WithDetail("%s outside granted mailboxes", what))
}

// listScope describes what an unfiltered inbox listing covers.
func listScope(session *auth.Session, q string) string {
	scope := "all granted mailboxes"
	if session.IsAdmin {
		scope = "all mailboxes (admin)"
	} else if len(session.Grants) > 0 {
		patterns := make([]string, 0, len(session.Grants))
		for _, g := range session.Grants {
			patterns = append(patterns, g.Pattern)
		}
		scope = "granted: " + strings.Join(patterns, ", ")
	}
	if strings.TrimSpace(q) != "" {
		scope += " (with search)"
	}
	return scope
}
