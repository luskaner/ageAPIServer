package models

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SteamID64 bounds of an individual Steam account. A ticket that carries a
// value in this range is taken as the account id.
const (
	steamID64Min uint64 = 76561197960265728
	steamID64Max uint64 = 76561202255233023
)

// User is the identity attached to a request.
type User struct {
	UserID      int64
	SteamID     string
	DisplayName string
	AvatarURL   string
}

// UserStore owns users and sessions.
type UserStore struct {
	db *sql.DB
}

// UserBySteamToken resolves the user behind a `Steam1.0 <token>` header,
// creating it on first sight. The ticket is not verified cryptographically yet:
// see the open risks in the plan.
func (s *UserStore) UserBySteamToken(ctx context.Context, token string) (*User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrUnauthorized
	}
	steamID := steamIDFromToken(token)
	return s.EnsureUser(ctx, steamID, "", "")
}

// EnsureUser returns the user with that Steam id, inserting it when missing.
func (s *UserStore) EnsureUser(ctx context.Context, steamID string, displayName string, avatarURL string) (*User, error) {
	if steamID == "" {
		return nil, ErrValidation
	}
	user, err := s.userBySteamID(ctx, steamID)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO users (steamId, displayName, avatarUrl) VALUES (?, NULLIF(?, ''), NULLIF(?, ''))`,
		steamID, displayName, avatarURL)
	if err != nil {
		// A concurrent request may have inserted the same Steam id first.
		if user, lookupErr := s.userBySteamID(ctx, steamID); lookupErr == nil {
			return user, nil
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("insert user id: %w", err)
	}
	return s.UserByID(ctx, id)
}

func (s *UserStore) userBySteamID(ctx context.Context, steamID string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT userId, COALESCE(steamId, ''), COALESCE(displayName, ''), COALESCE(avatarUrl, '')
		 FROM users WHERE steamId = ?`, steamID)
	return scanUser(row)
}

// UserByID returns one user.
func (s *UserStore) UserByID(ctx context.Context, userID int64) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT userId, COALESCE(steamId, ''), COALESCE(displayName, ''), COALESCE(avatarUrl, '')
		 FROM users WHERE userId = ?`, userID)
	return scanUser(row)
}

func scanUser(row *sql.Row) (*User, error) {
	var user User
	err := row.Scan(&user.UserID, &user.SteamID, &user.DisplayName, &user.AvatarURL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return &user, nil
}

// CreateSession issues a web session token for the user.
func (s *UserStore) CreateSession(ctx context.Context, userID int64, ttl time.Duration) (string, time.Time, error) {
	if userID <= 0 {
		return "", time.Time{}, ErrValidation
	}
	if ttl <= 0 {
		ttl = 720 * time.Hour
	}
	token, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	sessionID, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expires := now.Add(ttl)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (sessionId, userId, token, createdAt, expiresAt) VALUES (?, ?, ?, ?, ?)`,
		sessionID, userID, token, now.Format(time.RFC3339), expires.Format(time.RFC3339))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("insert session: %w", err)
	}
	return token, expires, nil
}

// UserBySessionToken resolves the user behind a session cookie value.
func (s *UserStore) UserBySessionToken(ctx context.Context, token string) (*User, error) {
	if token == "" {
		return nil, ErrUnauthorized
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT u.userId, COALESCE(u.steamId, ''), COALESCE(u.displayName, ''), COALESCE(u.avatarUrl, '')
		 FROM sessions s JOIN users u ON u.userId = s.userId
		 WHERE s.token = ? AND s.expiresAt > ?`,
		token, time.Now().UTC().Format(time.RFC3339))
	return scanUser(row)
}

// DeleteSession removes a session token.
func (s *UserStore) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// PurgeExpiredSessions drops the sessions that are past their expiry.
func (s *UserStore) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expiresAt <= ?`,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func randomToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

// steamIDFromToken extracts the SteamID64 from a Steam1.0 ticket. Only the
// gbe_fork ticket layouts are decoded (see gbeTicketSteamID): the tickets the
// real Steam client issues keep the account id inside an AES-256 Encrypted App
// Ticket that cannot be opened without the publisher key, so they stay opaque.
// When the token carries no readable id, a stable hash of the token is used so
// the same client keeps the same identity across restarts.
func steamIDFromToken(token string) string {
	for _, encoding := range steamTicketEncodings {
		decoded, err := encoding.DecodeString(token)
		if err != nil {
			continue
		}
		if steamID, ok := gbeTicketSteamID(decoded); ok {
			return strconv.FormatUint(steamID, 10)
		}
	}
	sum := sha256.Sum256([]byte(token))
	return "token-" + hex.EncodeToString(sum[:8])
}
