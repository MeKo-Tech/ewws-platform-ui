// Package middleware contains the small HTTP middlewares the server uses.
package middleware

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"

	"github.com/gorilla/sessions"
)

// SessionName is the cookie name used for the auth session.
const SessionName = "ewws_platform_session"

// SessionStore wraps the gorilla cookie store with our specific options.
type SessionStore struct {
	store *sessions.CookieStore
}

// NewSessionStore builds a SessionStore. `secret` must be at least 32 bytes
// (we use the first 32 as the AES-CTR encryption key and a HMAC key).
func NewSessionStore(secret []byte) *SessionStore {
	hashKey := secret

	var encKey []byte
	if len(secret) >= 32 {
		encKey = secret[:32]
	}

	cs := sessions.NewCookieStore(hashKey, encKey)
	cs.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   60 * 60 * 12,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}

	return &SessionStore{store: cs}
}

// Get returns the underlying session for the current request.
func (s *SessionStore) Get(r *http.Request) *sessions.Session {
	sess, _ := s.store.Get(r, SessionName)
	return sess
}

// Save persists the session back to the cookie.
func (s *SessionStore) Save(r *http.Request, w http.ResponseWriter, sess *sessions.Session) error {
	return s.store.Save(r, w, sess)
}

// SessionUser is the small user struct we keep server-side per session.
type SessionUser struct {
	Login   string
	Name    string
	Email   string
	IsAdmin bool
	Token   string // OAuth access token, base64 to keep gob-safe
}

type ctxKey int

const (
	userKey ctxKey = iota
	sessKey
)

// UserFromContext returns the SessionUser placed by the Session middleware.
func UserFromContext(ctx context.Context) *SessionUser {
	v, ok := ctx.Value(userKey).(*SessionUser)
	if !ok {
		return nil
	}

	return v
}

// SessionFromContext returns the *sessions.Session placed by the Session middleware.
func SessionFromContext(ctx context.Context) *sessions.Session {
	v, ok := ctx.Value(sessKey).(*sessions.Session)
	if !ok {
		return nil
	}

	return v
}

// Session is a middleware that loads the cookie session, decodes the
// stashed user if any, and injects both into the request context.
func Session(store *SessionStore, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess := store.Get(r)
			ctx := context.WithValue(r.Context(), sessKey, sess)

			if user, ok := decodeUser(sess); ok {
				ctx = context.WithValue(ctx, userKey, user)
			} else if sess.Values["login"] != nil {
				logger.Warn("session has login but decode failed")
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func decodeUser(sess *sessions.Session) (*SessionUser, bool) {
	login, _ := sess.Values["login"].(string)
	if login == "" {
		return nil, false
	}

	name, _ := sess.Values["name"].(string)
	email, _ := sess.Values["email"].(string)
	isAdmin, _ := sess.Values["isAdmin"].(bool)
	tokenB64, _ := sess.Values["token"].(string)

	var token string

	if tokenB64 != "" {
		if raw, err := base64.StdEncoding.DecodeString(tokenB64); err == nil {
			token = string(raw)
		}
	}

	return &SessionUser{
		Login:   login,
		Name:    name,
		Email:   email,
		IsAdmin: isAdmin,
		Token:   token,
	}, true
}

// StoreUser writes the user back into the session (call Save after).
func StoreUser(sess *sessions.Session, u *SessionUser) {
	sess.Values["login"] = u.Login
	sess.Values["name"] = u.Name
	sess.Values["email"] = u.Email
	sess.Values["isAdmin"] = u.IsAdmin
	sess.Values["token"] = base64.StdEncoding.EncodeToString([]byte(u.Token))
}

// ClearUser wipes user-related values.
func ClearUser(sess *sessions.Session) {
	for k := range sess.Values {
		delete(sess.Values, k)
	}
}
