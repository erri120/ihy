// Real-Postgres tests for the auth package: password hashing,
// login, session expiry, and API tokens.
package auth

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"ihy/internal/db"
	"ihy/internal/dbtest"
)

func setup(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := dbtest.Server(t)
	if _, err := db.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	created, err := EnsureAdminUser(context.Background(), conn, "admin", "hunter2!")
	if err != nil || !created {
		t.Fatalf("bootstrap admin: created=%v err=%v", created, err)
	}
	return context.Background(), conn
}

func TestPasswordHashRoundtrip(t *testing.T) {
	hash, err := HashPassword("s3cret/x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash format: %q", hash)
	}
	ok, err := VerifyPassword(hash, "s3cret/x")
	if err != nil || !ok {
		t.Fatalf("correct password rejected: ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword(hash, "wrong")
	if err != nil || ok {
		t.Fatalf("wrong password accepted: ok=%v", ok)
	}
}

func TestEnsureAdminUserIsIdempotent(t *testing.T) {
	ctx, conn := setup(t)
	created, err := EnsureAdminUser(ctx, conn, "admin", "different-pw")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("second bootstrap created another user")
	}
	// The original password must still work.
	if _, _, err := Login(ctx, conn, "admin", "hunter2!"); err != nil {
		t.Fatalf("original password no longer valid: %v", err)
	}
}

func TestLoginAndSession(t *testing.T) {
	ctx, conn := setup(t)

	// Wrong password is rejected with the same message as a missing
	// user (no account enumeration).
	if _, _, err := Login(ctx, conn, "admin", "wrong"); err == nil {
		t.Fatal("wrong password accepted")
	} else if !strings.Contains(err.Error(), "invalid credentials") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, _, err := Login(ctx, conn, "ghost", "whatever"); err == nil {
		t.Fatal("unknown user accepted")
	}

	raw, expires, err := Login(ctx, conn, "admin", "hunter2!")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !expires.After(time.Now()) {
		t.Fatalf("session already expired: %v", expires)
	}
	u, err := UserFromSession(ctx, conn, raw)
	if err != nil {
		t.Fatalf("session lookup: %v", err)
	}
	if u.Name != "admin" {
		t.Errorf("user name = %q, want admin", u.Name)
	}

	// Garbage tokens are rejected.
	if _, err := UserFromSession(ctx, conn, "not-a-token"); err == nil {
		t.Fatal("garbage session accepted")
	}
}

func TestExpiredSessionRejected(t *testing.T) {
	ctx, conn := setup(t)
	raw, _, err := Login(ctx, conn, "admin", "hunter2!")
	if err != nil {
		t.Fatal(err)
	}
	// Force-expire the session directly.
	if _, err := conn.Exec(`UPDATE session SET expires_at = now() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if _, err := UserFromSession(ctx, conn, raw); err == nil {
		t.Fatal("expired session accepted")
	}
}

func TestAPITokens(t *testing.T) {
	ctx, conn := setup(t)
	raw, expires, err := Login(ctx, conn, "admin", "hunter2!")
	if err != nil {
		t.Fatal(err)
	}
	u, err := UserFromSession(ctx, conn, raw)
	if err != nil {
		t.Fatal(err)
	}
	_ = expires

	token, err := CreateToken(ctx, conn, u.ID, "android")
	if err != nil {
		t.Fatal(err)
	}
	tu, err := UserFromToken(ctx, conn, token)
	if err != nil {
		t.Fatalf("token lookup: %v", err)
	}
	if tu.ID != u.ID {
		t.Fatalf("token resolves to wrong user")
	}

	tokens, err := ListTokens(ctx, conn, u.ID)
	if err != nil || len(tokens) != 1 || tokens[0].Name != "android" {
		t.Fatalf("list tokens: %+v err=%v", tokens, err)
	}

	if err := RevokeToken(ctx, conn, u.ID, tokens[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := UserFromToken(ctx, conn, token); err == nil {
		t.Fatal("revoked token still valid")
	}
	if err := RevokeToken(ctx, conn, u.ID, tokens[0].ID); err == nil {
		t.Fatal("revoking a foreign/nonexistent token succeeded")
	}
}
