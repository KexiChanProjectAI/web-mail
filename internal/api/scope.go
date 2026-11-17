package api

import (
	"strings"

	"lite-mail/internal/auth"
	"lite-mail/internal/db"
)

// scopeClause returns an SQL condition over messages alias "m" that limits rows
// to the mailboxes the session may read, or "" when the session is unrestricted.
// A message belongs to its envelope recipients (where Cloudflare delivered it),
// not to its To/Cc/Bcc headers, which the sender writes and forwarding keeps.
// mailbox, when non-empty, narrows to one canonical recipient address; callers
// must have checked session.HasMailboxAccess(mailbox) first.
func scopeClause(session *auth.Session, mailbox string) (string, []any) {
	pred, args := recipientPredicate(session, mailbox)
	if pred == "" {
		return "", nil
	}
	return "EXISTS (SELECT 1 FROM message_recipients mr WHERE mr.message_id = m.id AND " + db.MailboxRecipientCondition("mr", "m.id") + " AND " + pred + ")", args
}

// recipientPredicate builds the condition over message_recipients alias "mr".
// It never emits an empty IN list: a session with no grants gets "0 = 1".
func recipientPredicate(session *auth.Session, mailbox string) (string, []any) {
	if mailbox != "" {
		if session == nil || !session.HasMailboxAccess(mailbox) {
			return "0 = 1", nil
		}
		return "mr.recipient_email = ?", []any{mailbox}
	}
	if session != nil && session.IsAdmin {
		return "", nil
	}
	if session == nil || len(session.Grants) == 0 {
		return "0 = 1", nil
	}

	var exact []any
	var parts []string
	var args []any
	for _, g := range session.Grants {
		switch g.Kind {
		case auth.GrantExact:
			exact = append(exact, g.Value)
		case auth.GrantDomain:
			parts = append(parts, "mr.recipient_email LIKE ?")
			args = append(args, "%@"+db.EscapeLike(g.Value))
		}
	}
	if len(exact) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(exact)), ",")
		parts = append([]string{"mr.recipient_email IN (" + placeholders + ")"}, parts...)
		args = append(exact, args...)
	}
	if len(parts) == 0 {
		return "0 = 1", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// whereClause joins non-empty conditions into a WHERE clause.
func whereClause(conds ...string) string {
	var nonEmpty []string
	for _, c := range conds {
		if c != "" {
			nonEmpty = append(nonEmpty, c)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(nonEmpty, " AND ")
}
