package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"lite-mail/internal/auth"
	"lite-mail/internal/emailaddr"
)

var (
	ErrUserNotFound  = errors.New("user not found")
	ErrUserExists    = errors.New("user already exists")
	ErrGrantNotFound = errors.New("grant not found")
	ErrGrantExists   = errors.New("grant already exists")
)

const mysqlDuplicateEntry = 1062

// UserStore persists login identities and their mailbox grants. It never
// touches the database at construction time, so a nil *sql.DB is fine for
// routes that do not use it.
type UserStore struct {
	db *sql.DB
}

func NewUserStore(database *sql.DB) *UserStore {
	return &UserStore{db: database}
}

var _ auth.PrincipalSource = (*UserStore)(nil)
var _ auth.LoginRecorder = (*UserStore)(nil)

// LookupPrincipal returns admin flag, grants and last login for email, or (nil, nil).
func (s *UserStore) LookupPrincipal(ctx context.Context, email string) (*auth.PrincipalRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.is_admin, u.last_login_at, g.pattern
FROM users u LEFT JOIN mailbox_grants g ON g.user_id = u.id
WHERE u.email = ? ORDER BY g.id`, emailaddr.Canonical(email))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rec *auth.PrincipalRecord
	for rows.Next() {
		var (
			id        int64
			isAdmin   bool
			lastLogin sql.NullTime
			pattern   sql.NullString
		)
		if err := rows.Scan(&id, &isAdmin, &lastLogin, &pattern); err != nil {
			return nil, err
		}
		if rec == nil {
			rec = &auth.PrincipalRecord{UserID: id, IsAdmin: isAdmin}
			if lastLogin.Valid {
				t := lastLogin.Time
				rec.LastLoginAt = &t
			}
		}
		if pattern.Valid {
			rec.Patterns = append(rec.Patterns, pattern.String)
		}
	}
	return rec, rows.Err()
}

// RecordLogin creates the users row on first sight and stamps the login.
func (s *UserStore) RecordLogin(ctx context.Context, email, provider string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (email, last_login_at, last_login_provider) VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE last_login_at = VALUES(last_login_at), last_login_provider = VALUES(last_login_provider)`,
		emailaddr.Canonical(email), time.Now().UTC(), provider)
	return err
}

const userColumns = "id, email, is_admin, created_at, last_login_at, last_login_provider"

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.IsAdmin, &u.CreatedAt, &u.LastLoginAt, &u.LastLoginProvider); err != nil {
		return nil, err
	}
	return &u, nil
}

// ListUsers returns all users ordered by email.
func (s *UserStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+userColumns+" FROM users ORDER BY email")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

// ListGrants returns every grant ordered by user and id.
func (s *UserStore) ListGrants(ctx context.Context) ([]MailboxGrant, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, user_id, pattern, created_at, created_by FROM mailbox_grants ORDER BY user_id, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grants := []MailboxGrant{}
	for rows.Next() {
		var g MailboxGrant
		if err := rows.Scan(&g.ID, &g.UserID, &g.Pattern, &g.CreatedAt, &g.CreatedBy); err != nil {
			return nil, err
		}
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

func (s *UserStore) FindUser(ctx context.Context, id int64) (*User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return u, err
}

// CreateUser adds a user ahead of their first login so grants can be attached.
func (s *UserStore) CreateUser(ctx context.Context, email string) (*User, error) {
	res, err := s.db.ExecContext(ctx, "INSERT INTO users (email) VALUES (?)", emailaddr.Canonical(email))
	if isDuplicate(err) {
		return nil, ErrUserExists
	}
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.FindUser(ctx, id)
}

func (s *UserStore) SetAdmin(ctx context.Context, id int64, isAdmin bool) error {
	if _, err := s.FindUser(ctx, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, "UPDATE users SET is_admin = ? WHERE id = ?", isAdmin, id)
	return err
}

// DeleteUser removes a user; grants cascade.
func (s *UserStore) DeleteUser(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// AddGrant stores a canonical pattern (validate it with auth.ParseGrant first).
func (s *UserStore) AddGrant(ctx context.Context, userID int64, pattern, createdBy string) (*MailboxGrant, error) {
	if _, err := s.FindUser(ctx, userID); err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, "INSERT INTO mailbox_grants (user_id, pattern, created_by) VALUES (?, ?, ?)", userID, pattern, nullString(createdBy))
	if isDuplicate(err) {
		return nil, ErrGrantExists
	}
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	var g MailboxGrant
	err = s.db.QueryRowContext(ctx, "SELECT id, user_id, pattern, created_at, created_by FROM mailbox_grants WHERE id = ?", id).
		Scan(&g.ID, &g.UserID, &g.Pattern, &g.CreatedAt, &g.CreatedBy)
	return &g, err
}

// FindGrant returns grantID only if it belongs to userID.
func (s *UserStore) FindGrant(ctx context.Context, userID, grantID int64) (*MailboxGrant, error) {
	var g MailboxGrant
	err := s.db.QueryRowContext(ctx, "SELECT id, user_id, pattern, created_at, created_by FROM mailbox_grants WHERE id = ? AND user_id = ?", grantID, userID).
		Scan(&g.ID, &g.UserID, &g.Pattern, &g.CreatedAt, &g.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGrantNotFound
	}
	return &g, err
}

// DeleteGrant removes grantID only if it belongs to userID.
func (s *UserStore) DeleteGrant(ctx context.Context, userID, grantID int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM mailbox_grants WHERE id = ? AND user_id = ?", grantID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrGrantNotFound
	}
	return nil
}

// MailboxRecipientCondition is an SQL condition over message_recipients alias
// rAlias that keeps the rows deciding which mailbox a message belongs to: its
// envelope recipients, or its To/Cc/Bcc rows when no envelope recipient was
// recorded (mail ingested before envelope tracking that could not be backfilled).
// messageIDExpr is the SQL expression for the message id in the outer query.
func MailboxRecipientCondition(rAlias, messageIDExpr string) string {
	return "(" + rAlias + ".recipient_type = 'envelope' OR NOT EXISTS (SELECT 1 FROM message_recipients env WHERE env.message_id = " +
		messageIDExpr + " AND env.recipient_type = 'envelope'))"
}

// KnownMailboxes lists recipient addresses and domains seen in stored mail,
// optionally filtered by a substring, for the admin grant editor.
func (s *UserStore) KnownMailboxes(ctx context.Context, q string, limit int) (addresses, domains []string, err error) {
	like := "%" + EscapeLike(strings.ToLower(strings.TrimSpace(q))) + "%"
	addresses, err = s.stringColumn(ctx, `SELECT DISTINCT mr.recipient_email FROM message_recipients mr
WHERE `+MailboxRecipientCondition("mr", "mr.message_id")+` AND mr.recipient_email LIKE ? ORDER BY mr.recipient_email LIMIT ?`, like, limit)
	if err != nil {
		return nil, nil, err
	}
	domains, err = s.stringColumn(ctx, `SELECT DISTINCT SUBSTRING_INDEX(mr.recipient_email, '@', -1) AS d FROM message_recipients mr
WHERE `+MailboxRecipientCondition("mr", "mr.message_id")+` AND mr.recipient_email LIKE ? ORDER BY d LIMIT ?`, like, limit)
	return addresses, domains, err
}

func (s *UserStore) stringColumn(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// EscapeLike escapes the LIKE metacharacters \, % and _ (default escape char '\').
func EscapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func isDuplicate(err error) bool {
	var myErr *mysql.MySQLError
	return errors.As(err, &myErr) && myErr.Number == mysqlDuplicateEntry
}

func nullString(v string) sql.NullString {
	return sql.NullString{String: v, Valid: v != ""}
}
