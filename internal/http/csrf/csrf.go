// Package csrf is a double-submit CSRF helper used by the /claim POST.
//
// The pattern:
//  1. On GET /claim we mint a random token, set it as a (Lax, HttpOnly=false)
//     cookie AND embed it as a hidden input.
//  2. On POST we compare the form field to the cookie. Match → ok.
//
// This is the cheapest CSRF defense for an HTMX/SSR app and is fine for a
// Tailscale-gated internal tool. We do not bother CSRF-protecting GET routes.
package csrf

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
)

// CookieName is the cookie that carries the double-submit token.
const CookieName = "ewws_csrf"

// MintToken sets the CSRF cookie on the response and returns the token
// to embed as a hidden form field.
func MintToken(w http.ResponseWriter, r *http.Request) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	token := base64.RawURLEncoding.EncodeToString(buf)

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Secure:   r.TLS != nil,
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 30,
	})

	return token, nil
}

// Verify checks that the form field "csrf" matches the cookie.
func Verify(r *http.Request) error {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return errors.New("missing csrf cookie")
	}

	formToken := r.FormValue("csrf")
	if formToken == "" {
		return errors.New("missing csrf form field")
	}

	if cookie.Value != formToken {
		return errors.New("csrf mismatch")
	}

	return nil
}
