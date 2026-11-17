package db

import (
	"database/sql"
	"time"
)

// User is a login identity known to lite-mail.
type User struct {
	ID                int64
	Email             string
	IsAdmin           bool
	CreatedAt         time.Time
	LastLoginAt       sql.NullTime
	LastLoginProvider sql.NullString
}

// MailboxGrant lets a user read one recipient address or a whole domain.
type MailboxGrant struct {
	ID        int64
	UserID    int64
	Pattern   string
	CreatedAt time.Time
	CreatedBy sql.NullString
}
