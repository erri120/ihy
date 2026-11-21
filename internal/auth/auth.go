// Package auth provides password hashing, sessions and API tokens
// for ihy.
//
// Account model (docs/scope.md Runtime decisions): a single
// auto-created "admin" account, bootstrapped from the environment; no
// self-registration; OAuth is planned to replace password flows later.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// HashPassword hashes a password with argon2id and returns the PHC
// string representation.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 2, 64*1024, 4, 32)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, 64*1024, 2, 4,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks a password against a PHC argon2id hash in
// constant time.
func VerifyPassword(phcHash, password string) (bool, error) {
	parts := strings.Split(phcHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, fmt.Errorf("unsupported hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, err
	}
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// User is a resolved authenticated identity.
type User struct {
	ID   int64
	Name string
}

func newSecret() (raw, hash string) {
	buf := make([]byte, 32)
	rand.Read(buf)
	raw = hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(sum[:])
}

// EnsureAdminUser creates the single admin account when no user
// exists at all. Idempotent: an existing user is left untouched.
// Returns whether a user was created.
func EnsureAdminUser(ctx context.Context, db *sql.DB, name, password string) (bool, error) {
	if password == "" {
		return false, fmt.Errorf("admin password must not be empty")
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM "user"`).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	hash, err := HashPassword(password)
	if err != nil {
		return false, err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO "user" (name, password_hash) VALUES ($1, $2)`, name, hash)
	return err == nil, err
}

// Login verifies credentials and opens a session, returning the raw
// session token for the cookie (only the SHA-256 is stored).
func Login(ctx context.Context, db *sql.DB, name, password string) (string, time.Time, error) {
	var phc string
	var userID int64
	err := db.QueryRowContext(ctx,
		`SELECT id, password_hash FROM "user" WHERE name = $1`, name).Scan(&userID, &phc)
	if err == sql.ErrNoRows {
		return "", time.Time{}, fmt.Errorf("invalid credentials")
	}
	if err != nil {
		return "", time.Time{}, err
	}
	ok, err := VerifyPassword(phc, password)
	if err != nil {
		return "", time.Time{}, err
	}
	if !ok {
		return "", time.Time{}, fmt.Errorf("invalid credentials")
	}
	return openSession(ctx, db, userID)
}

// openSession stores a session and returns its raw token.
func openSession(ctx context.Context, db *sql.DB, userID int64) (string, time.Time, error) {
	raw, hash := newSecret()
	expires := time.Now().Add(30 * 24 * time.Hour).UTC()
	_, err := db.ExecContext(ctx,
		`INSERT INTO session (token, user_id, expires_at) VALUES ($1, $2, $3)`,
		hash, userID, expires)
	if err != nil {
		return "", time.Time{}, err
	}
	return raw, expires, nil
}

// UserFromSession resolves a session cookie value to a user, if the
// session exists and is unexpired.
func UserFromSession(ctx context.Context, db *sql.DB, rawToken string) (User, error) {
	return userFromSecret(ctx, db, rawToken, "session")
}

// UserFromToken resolves a bearer API token to a user.
func UserFromToken(ctx context.Context, db *sql.DB, rawToken string) (User, error) {
	return userFromSecret(ctx, db, rawToken, "api_token")
}

func userFromSecret(ctx context.Context, db *sql.DB, rawToken, table string) (User, error) {
	sum := sha256.Sum256([]byte(rawToken))
	hash := hex.EncodeToString(sum[:])
	// The session table stores its hash in token, the api_token table
	// in token_hash. API tokens are valid until revoked.
	query := fmt.Sprintf(`SELECT s.user_id, u.name FROM %s s JOIN "user" u ON u.id = s.user_id
		WHERE s.%s = $1`, table, map[string]string{
		"session":   "token",
		"api_token": "token_hash",
	}[table])
	if table == "session" {
		query += ` AND s.expires_at > now()`
	}
	var u User
	err := db.QueryRowContext(ctx, query, hash).Scan(&u.ID, &u.Name)
	if err == sql.ErrNoRows {
		return User{}, fmt.Errorf("invalid or expired credentials")
	}
	return u, err
}

// CreateToken issues an API token for a user and returns the raw
// token exactly once; only its hash is stored.
func CreateToken(ctx context.Context, db *sql.DB, userID int64, name string) (string, error) {
	raw, hash := newSecret()
	_, err := db.ExecContext(ctx,
		`INSERT INTO api_token (user_id, token_hash, name) VALUES ($1, $2, $3)`,
		userID, hash, name)
	if err != nil {
		return "", err
	}
	return raw, nil
}

// RevokeToken deletes one of the user's API tokens by id.
func RevokeToken(ctx context.Context, db *sql.DB, userID, tokenID int64) error {
	res, err := db.ExecContext(ctx,
		`DELETE FROM api_token WHERE id = $1 AND user_id = $2`, tokenID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("token not found")
	}
	return nil
}

// ListTokens returns the user's API tokens (id and name only, never
// the raw secret).
func ListTokens(ctx context.Context, db *sql.DB, userID int64) ([]TokenInfo, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, name, created_at FROM api_token WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TokenInfo
	for rows.Next() {
		var t TokenInfo
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TokenInfo describes an API token without its secret.
type TokenInfo struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}
