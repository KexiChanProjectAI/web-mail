package auth

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"lite-mail/internal/config"
)

func TestSessionCreateAndRetrieve(t *testing.T) {
	store := NewCookieSessionStore("test_session")
	session := &Session{Email: "user@example.test", ExpiresAt: time.Now().Add(time.Hour)}

	if err := store.Create(session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if session.Token == "" {
		t.Fatal("Create() did not assign token")
	}

	got, err := store.Get(session.Token)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Email != session.Email || got.Token != session.Token || got.IsAdmin != session.IsAdmin {
		t.Fatalf("Get() = %+v, want %+v", got, session)
	}

	got.Email = "mutated@example.test"
	again, err := store.Get(session.Token)
	if err != nil {
		t.Fatalf("Get() second error = %v", err)
	}
	if again.Email != session.Email {
		t.Fatalf("stored session mutated through returned pointer: %q", again.Email)
	}
}

func TestSessionExpiration(t *testing.T) {
	store := NewCookieSessionStore("test_session")
	session := &Session{Email: "user@example.test", ExpiresAt: time.Now().Add(-time.Hour)}
	if err := store.Create(session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := store.Get(session.Token)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !time.Now().After(got.ExpiresAt) {
		t.Fatalf("ExpiresAt = %v, want expired", got.ExpiresAt)
	}
}

func TestTokenGenerationUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for range 1000 {
		token, err := GenerateToken()
		if err != nil {
			t.Fatalf("GenerateToken() error = %v", err)
		}
		if len(token) != 64 {
			t.Fatalf("len(token) = %d, want 64", len(token))
		}
		if seen[token] {
			t.Fatalf("duplicate token generated: %s", token)
		}
		seen[token] = true
	}
}

func TestConcurrentAccessSafety(t *testing.T) {
	store := NewCookieSessionStore("test_session")
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			session := &Session{Email: "user@example.test", ExpiresAt: time.Now().Add(time.Hour)}
			if err := store.Create(session); err != nil {
				t.Errorf("Create() error = %v", err)
				return
			}
			if _, err := store.Get(session.Token); err != nil {
				t.Errorf("Get() error = %v", err)
			}
			if i%2 == 0 {
				if err := store.Delete(session.Token); err != nil {
					t.Errorf("Delete() error = %v", err)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestSessionCookieAttributes(t *testing.T) {
	cfg := &config.Config{SessionCookieName: "custom", SessionTTLHours: 2, AppEnv: "production"}
	expires := time.Now().Add(2 * time.Hour)
	cookie := NewSessionCookie(cfg, "token", expires)

	if cookie.Name != "custom" || cookie.Value != "token" {
		t.Fatalf("cookie name/value = %q/%q", cookie.Name, cookie.Value)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("cookie security attrs = %+v", cookie)
	}
	if cookie.MaxAge != 7200 || !cookie.Expires.Equal(expires) {
		t.Fatalf("cookie expiry = MaxAge %d Expires %v", cookie.MaxAge, cookie.Expires)
	}

	cfg.AppEnv = "development"
	if devCookie := NewSessionCookie(cfg, "token", expires); devCookie.Secure {
		t.Fatal("development cookie Secure = true, want false")
	}
}
