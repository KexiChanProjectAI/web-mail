package audit

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// Filter narrows a listing. Empty fields match everything.
type Filter struct {
	Actor    string // substring of the login email
	Mailbox  string // substring of the mailbox
	Event    string // exact event name
	Session  string // exact session key
	Since    time.Time
	Until    time.Time
	BeforeID int64 // cursor: only events with a smaller id
	Limit    int
}

// List returns events newest first.
func (l *Logger) List(ctx context.Context, f Filter) ([]Event, error) {
	if l == nil || l.db == nil {
		return []Event{}, nil
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	var where []string
	var args []any
	if f.Actor != "" {
		where = append(where, "actor_email LIKE ?")
		args = append(args, "%"+escapeLike(strings.ToLower(f.Actor))+"%")
	}
	if f.Mailbox != "" {
		where = append(where, "mailbox LIKE ?")
		args = append(args, "%"+escapeLike(strings.ToLower(f.Mailbox))+"%")
	}
	if f.Event != "" {
		where = append(where, "event = ?")
		args = append(args, f.Event)
	}
	if f.Session != "" {
		where = append(where, "session_key = ?")
		args = append(args, f.Session)
	}
	if !f.Since.IsZero() {
		where = append(where, "occurred_at >= ?")
		args = append(args, f.Since.UTC())
	}
	if !f.Until.IsZero() {
		where = append(where, "occurred_at < ?")
		args = append(args, f.Until.UTC())
	}
	if f.BeforeID > 0 {
		where = append(where, "id < ?")
		args = append(args, f.BeforeID)
	}
	query := `SELECT id, occurred_at, actor_email, provider, session_key, event,
COALESCE(mailbox, ''), COALESCE(message_id, 0), COALESCE(detail, ''), COALESCE(client_ip, ''), COALESCE(user_agent, ''), COALESCE(request_id, '')
FROM audit_events`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, f.Limit)

	rows, err := l.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Time, &e.Actor, &e.Provider, &e.SessionKey, &e.Event,
			&e.Mailbox, &e.MessageID, &e.Detail, &e.ClientIP, &e.UserAgent, &e.RequestID); err != nil {
			return nil, err
		}
		e.Time = e.Time.UTC()
		events = append(events, e)
	}
	return events, rows.Err()
}

// Prune deletes events older than retention in small batches and returns
// how many rows went.
func (l *Logger) Prune(ctx context.Context, retention time.Duration) (int64, error) {
	if l == nil || l.db == nil || retention <= 0 {
		return 0, nil
	}
	cutoff := l.now().UTC().Add(-retention)
	var total int64
	for {
		res, err := l.db.ExecContext(ctx, "DELETE FROM audit_events WHERE occurred_at < ? ORDER BY id LIMIT 5000", cutoff)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < 5000 {
			return total, nil
		}
	}
}

// StartPruner prunes once an hour until ctx ends.
func (l *Logger) StartPruner(ctx context.Context, retention time.Duration) {
	if l == nil || l.db == nil || retention <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			if n, err := l.Prune(ctx, retention); err != nil {
				slog.Error("audit prune failed", "error", err.Error())
			} else if n > 0 {
				slog.Info("audit pruned", "rows", n, "retention_days", int(retention.Hours()/24))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
