package auth

import (
	"context"
	"time"

	"lite-mail/internal/config"
	"lite-mail/internal/emailaddr"
)

// Identity is what a credential proves about the caller.
type Identity struct {
	Email    string
	Provider string
}

// PrincipalRecord is what the database knows about a login email.
type PrincipalRecord struct {
	UserID      int64
	IsAdmin     bool
	Patterns    []string
	LastLoginAt *time.Time
}

// PrincipalSource looks up a login email. It returns (nil, nil) when the email
// has no users row.
type PrincipalSource interface {
	LookupPrincipal(ctx context.Context, email string) (*PrincipalRecord, error)
}

// LoginRecorder upserts a users row and stamps the last login.
type LoginRecorder interface {
	RecordLogin(ctx context.Context, email, provider string) error
}

// Resolver turns a proven identity into an authorized Session. Nothing is
// granted implicitly: a user without grants and without admin rights can sign
// in but reads no mailbox.
type Resolver struct {
	users PrincipalSource
	cfg   *config.Config
}

func NewResolver(users PrincipalSource, cfg *config.Config) *Resolver {
	return &Resolver{users: users, cfg: cfg}
}

// Resolve loads admin status and grants for id. The returned record (may be
// nil) lets callers decide whether to stamp a login.
func (r *Resolver) Resolve(ctx context.Context, id Identity) (*Session, *PrincipalRecord, error) {
	if id.Provider == ProviderAdminPSK {
		return &Session{Email: "admin", IsAdmin: true, Provider: ProviderAdminPSK}, nil, nil
	}
	email := emailaddr.Canonical(id.Email)
	session := &Session{Email: email, Provider: id.Provider}
	if r.cfg != nil && r.cfg.IsEnvAdmin(email) {
		session.IsAdmin = true
		session.IsEnvAdmin = true
	}
	if r.users == nil {
		return session, nil, nil
	}
	rec, err := r.users.LookupPrincipal(ctx, email)
	if err != nil {
		return nil, nil, err
	}
	if rec != nil {
		session.UserID = rec.UserID
		session.IsAdmin = session.IsAdmin || rec.IsAdmin
		session.Grants = ParseGrants(rec.Patterns)
	}
	return session, rec, nil
}
