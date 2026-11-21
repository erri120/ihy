// HTTP auth surface: login/logout, API token management, and the
// auth middleware. Hand-rolled for now; the OpenAPI-generated
// handlers will absorb these routes as the API spec lands.
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ihy/internal/auth"
)

const sessionCookie = "ihy_session"

// Auth holds the handlers and middleware for authentication.
type Auth struct {
	db *sql.DB
}

func NewAuth(db *sql.DB) *Auth { return &Auth{db: db} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Login handles POST /auth/login: username+password in, session
// cookie out.
func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	raw, expires, err := auth.Login(r.Context(), a.db, req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Logout handles POST /auth/logout: clears the cookie. Session
// revocation server-side arrives with the session-store DELETE; for
// now the cookie is dropped client-side.
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// CreateToken handles POST /auth/tokens (authenticated): issues an
// API token. The raw token is returned exactly once.
func (a *Auth) CreateToken(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}
	if req.Name == "" {
		req.Name = "unnamed"
	}
	raw, err := auth.CreateToken(r.Context(), a.db, user.ID, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token creation failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"token": raw, "name": req.Name})
}

// ListTokens handles GET /auth/tokens (authenticated).
func (a *Auth) ListTokens(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tokens, err := auth.ListTokens(r.Context(), a.db, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing failed")
		return
	}
	type out struct {
		ID        int64     `json:"id"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
	}
	list := make([]out, 0, len(tokens))
	for _, t := range tokens {
		list = append(list, out{ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt})
	}
	writeJSON(w, http.StatusOK, list)
}

// RevokeToken handles DELETE /auth/tokens/{id} (authenticated).
func (a *Auth) RevokeToken(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	pathID := strings.TrimPrefix(r.URL.Path, "/auth/tokens/")
	id, err := strconv.ParseInt(pathID, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid token id")
		return
	}
	if err = auth.RevokeToken(r.Context(), a.db, user.ID, id); err != nil {
		writeError(w, http.StatusNotFound, "token not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type ctxKey int

const userKey ctxKey = 1

// UserFromContext returns the authenticated user, if any.
func UserFromContext(ctx context.Context) (auth.User, bool) {
	u, ok := ctx.Value(userKey).(auth.User)
	return u, ok
}

// publicAuthPaths are the only auth routes reachable without a
// session or token.
var publicAuthPaths = map[string]bool{
	"/auth/login":  true,
	"/auth/logout": true,
}

// Middleware authenticates a request via session cookie or bearer
// API token and stores the user in the context. Unauthenticated
// requests get a 401, except the public auth routes.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !publicAuthPaths[r.URL.Path] {
			var u auth.User
			var err error
			if c, cerr := r.Cookie(sessionCookie); cerr == nil {
				u, err = auth.UserFromSession(r.Context(), a.db, c.Value)
			} else if authz := r.Header.Get("Authorization"); strings.HasPrefix(authz, "Bearer ") {
				u, err = auth.UserFromToken(r.Context(), a.db, strings.TrimPrefix(authz, "Bearer "))
			} else {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid or expired credentials")
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), userKey, u))
		}
		next.ServeHTTP(w, r)
	})
}
