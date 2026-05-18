package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/config"
	gh "github.com/MeKo-Tech/ewws-platform-ui/internal/github"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/middleware"
	"golang.org/x/oauth2"
	githuboauth "golang.org/x/oauth2/github"
)

// Auth wires the OAuth2 login / logout / callback routes.
type Auth struct {
	Cfg          *config.Config
	SessionStore *middleware.SessionStore
	Logger       *slog.Logger
}

const stateCookieName = "ewws_oauth_state"

func (a Auth) oauthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     a.Cfg.GitHubClientID,
		ClientSecret: a.Cfg.GitHubClientSecret,
		Endpoint:     githuboauth.Endpoint,
		RedirectURL:  a.Cfg.OAuthRedirectURL(),
		Scopes:       []string{"read:user", "user:email", "read:org", "repo"},
	}
}

// Login starts the OAuth authorization-code flow.
func (a Auth) Login(w http.ResponseWriter, r *http.Request) {
	state := randomState()

	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   600,
		Secure:   r.TLS != nil,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	url := a.oauthConfig().AuthCodeURL(state, oauth2.AccessTypeOnline)

	http.Redirect(w, r, url, http.StatusSeeOther)
}

// Callback completes the OAuth flow, fetches user + team membership,
// stores the result in the session, and redirects home.
func (a Auth) Callback(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	cookie, err := r.Cookie(stateCookieName)
	if err != nil || cookie.Value == "" {
		http.Error(w, "missing state", http.StatusBadRequest)
		return
	}

	if r.URL.Query().Get("state") != cookie.Value {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	clearCookie(w, stateCookieName)

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	cfg := a.oauthConfig()

	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		a.Logger.Error("oauth exchange", slog.Any("err", err))
		http.Error(w, "oauth exchange failed", http.StatusBadGateway)

		return
	}

	httpClient := cfg.Client(ctx, tok)

	user, err := gh.FetchUser(ctx, httpClient)
	if err != nil {
		a.Logger.Error("fetch user", slog.Any("err", err))
		http.Error(w, "fetch user failed", http.StatusBadGateway)

		return
	}

	// Org membership = baseline allowed.
	allowed, err := gh.IsOrgMember(ctx, httpClient, a.Cfg.AllowedOrg, user.Login)
	if err != nil {
		a.Logger.Warn("org check failed", slog.Any("err", err))
	}

	if !allowed {
		http.Error(w, "you must be a member of "+a.Cfg.AllowedOrg+" to use this UI", http.StatusForbidden)
		return
	}

	isAdmin, err := gh.IsTeamMember(ctx, httpClient, a.Cfg.AllowedOrg, a.Cfg.AdminTeam, user.Login)
	if err != nil {
		a.Logger.Warn("admin team check failed", slog.Any("err", err))
	}

	sess := a.SessionStore.Get(r)

	middleware.StoreUser(sess, &middleware.SessionUser{
		Login:   user.Login,
		Name:    user.Name,
		Email:   user.Email,
		IsAdmin: isAdmin,
		Token:   tok.AccessToken,
	})

	if err := a.SessionStore.Save(r, w, sess); err != nil {
		a.Logger.Error("save session", slog.Any("err", err))
		http.Error(w, "session save failed", http.StatusInternalServerError)

		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Logout clears the session and bounces back to the landing page.
func (a Auth) Logout(w http.ResponseWriter, r *http.Request) {
	sess := a.SessionStore.Get(r)

	middleware.ClearUser(sess)
	sess.Options.MaxAge = -1

	if err := a.SessionStore.Save(r, w, sess); err != nil {
		a.Logger.Warn("logout save", slog.Any("err", err))
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func randomState() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)

	return base64.RawURLEncoding.EncodeToString(buf)
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:   name,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

// Sanitize accepts user-supplied text and trims it for safe display.
func Sanitize(s string) string {
	return strings.TrimSpace(s)
}
