// HTTP tests for the auth surface: middleware, login, tokens.
// Uses httptest with a real Postgres (IHY_TEST_DSN).
package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"ihy/internal/auth"
	"ihy/internal/db"
	"ihy/internal/dbtest"
)

func setup(t *testing.T) (*Auth, *httptest.Server) {
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
	if created, err := auth.EnsureAdminUser(context.Background(), conn, "admin", "hunter2!"); err != nil || !created {
		t.Fatalf("bootstrap: created=%v err=%v", created, err)
	}
	a := NewAuth(conn)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/login", a.Login)
	mux.HandleFunc("POST /auth/logout", a.Logout)
	mux.Handle("POST /auth/tokens", a.Middleware(http.HandlerFunc(a.CreateToken)))
	mux.Handle("GET /auth/tokens", a.Middleware(http.HandlerFunc(a.ListTokens)))
	mux.Handle("DELETE /auth/tokens/{id}", a.Middleware(http.HandlerFunc(a.RevokeToken)))
	return a, httptest.NewServer(mux)
}

func postJSON(t *testing.T, url, body string) (*http.Response, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return resp, out
}

func TestHTTPAuthFlow(t *testing.T) {
	_, srv := setup(t)

	// Unauthenticated token create must fail.
	resp, out := postJSON(t, srv.URL+"/auth/tokens", `{"name":"x"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create: %d %v", resp.StatusCode, out)
	}

	// Wrong password.
	resp, _ = postJSON(t, srv.URL+"/auth/login", `{"username":"admin","password":"nope"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}

	// Login obtains a session cookie.
	client := &http.Client{}
	resp, _ = postJSON(t, srv.URL+"/auth/login", `{"username":"admin","password":"hunter2!"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookie {
		t.Fatalf("no session cookie: %v", cookies)
	}

	// Session cookie authenticates token creation; raw token comes
	// back exactly once.
	req, _ := http.NewRequest("POST", srv.URL+"/auth/tokens",
		bytes.NewReader([]byte(`{"name":"android"}`)))
	req.AddCookie(cookies[0])
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var tokOut map[string]any
	json.NewDecoder(resp.Body).Decode(&tokOut)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("token create: %d %v", resp.StatusCode, tokOut)
	}
	raw, _ := tokOut["token"].(string)
	if raw == "" {
		t.Fatal("no raw token in response")
	}

	// The bearer token authenticates.
	req, _ = http.NewRequest("GET", srv.URL+"/auth/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(list) != 1 {
		t.Fatalf("bearer list: %d %v", resp.StatusCode, list)
	}

	// After revocation the token stops working.
	id := int64(list[0]["id"].(float64))
	req, _ = http.NewRequest("DELETE", srv.URL+"/auth/tokens/"+itoa(id), nil)
	req.AddCookie(cookies[0])
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke: %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/auth/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token still valid: %d", resp.StatusCode)
	}
}

func itoa(n int64) string {
	return fmt.Sprintf("%d", n)
}
