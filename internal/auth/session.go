// Package auth owns sessions, proving control of an email, and the access tiers
// that decide what a given caller may see.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
)

const (
	keyUserID     = "user_id"
	keyVerifiedAt = "verified_at"
	keyMergeFrom  = "merge_from" // pending cross-device claim, see challenge.go
)

const (
	sessionIdleTimeout = 365 * 24 * time.Hour
	sessionLifetime    = 10 * 365 * 24 * time.Hour
)

func NewSessionManager(db *sql.DB, secureCookies bool) *scs.SessionManager {
	m := scs.New()
	m.Store = &sessionStore{db: db}
	m.IdleTimeout = sessionIdleTimeout
	m.Lifetime = sessionLifetime

	m.Cookie.Name = "openjoga_session"
	m.Cookie.HttpOnly = true
	m.Cookie.Path = "/"
	m.Cookie.Secure = secureCookies
	m.Cookie.SameSite = http.SameSiteLaxMode

	return m
}

func UserID(ctx context.Context, m *scs.SessionManager) int64 {
	return m.GetInt64(ctx, keyUserID)
}

func SetUser(ctx context.Context, m *scs.SessionManager, userID int64, verified bool) error {
	if err := m.RenewToken(ctx); err != nil {
		return err
	}
	m.Put(ctx, keyUserID, userID)
	if verified {
		m.Put(ctx, keyVerifiedAt, time.Now().UTC().Format(time.RFC3339))
	}
	return nil
}

func VerifiedAt(ctx context.Context, m *scs.SessionManager) (time.Time, bool) {
	raw := m.GetString(ctx, keyVerifiedAt)
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, raw)
	return t, err == nil
}

func Logout(ctx context.Context, m *scs.SessionManager) error {
	return m.Destroy(ctx)
}

func SetPendingMerge(ctx context.Context, m *scs.SessionManager, fromUserID int64) {
	m.Put(ctx, keyMergeFrom, fromUserID)
}

func PendingMerge(ctx context.Context, m *scs.SessionManager) int64 {
	return m.GetInt64(ctx, keyMergeFrom)
}

func ClearPendingMerge(ctx context.Context, m *scs.SessionManager) {
	m.Remove(ctx, keyMergeFrom)
}

type sessionStore struct{ db *sql.DB }

func (s *sessionStore) Find(token string) ([]byte, bool, error) {
	var data []byte
	err := s.db.QueryRow(
		`SELECT data FROM sessions WHERE token = ? AND expiry > ?`,
		token, unixSeconds(time.Now())).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (s *sessionStore) Commit(token string, data []byte, expiry time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO sessions (token, data, expiry) VALUES (?,?,?)
		ON CONFLICT (token) DO UPDATE SET data = excluded.data, expiry = excluded.expiry`,
		token, data, unixSeconds(expiry))
	return err
}

func (s *sessionStore) Delete(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func DeleteExpiredSessions(db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE expiry < ?`, unixSeconds(time.Now()))
	return err
}

func unixSeconds(t time.Time) float64 {
	return float64(t.UnixNano()) / float64(time.Second)
}
