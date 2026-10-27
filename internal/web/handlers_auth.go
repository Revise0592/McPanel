package web

import (
	"crypto/subtle"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/Revise0592/McPanel/internal/auth"
	"github.com/Revise0592/McPanel/internal/store"
)

const minPassword = 8

func (a *App) setupPage(w http.ResponseWriter, r *http.Request) {
	if a.Store.Settings().PasswordHash != "" {
		back(w, r, "/login")
		return
	}
	a.render(w, r, "setup", "Set up", "", nil)
}

func (a *App) setupSubmit(w http.ResponseWriter, r *http.Request) {
	if a.Store.Settings().PasswordHash != "" {
		back(w, r, "/login")
		return
	}
	ip := clientIP(r)
	if err := a.limiter.Check(ip); err != nil {
		setError(w, err.Error())
		back(w, r, "/setup")
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if subtle.ConstantTimeCompare([]byte(strings.ToUpper(code)), []byte(a.SetupCode)) != 1 {
		a.limiter.Fail(ip)
		setError(w, "That setup code isn't right. It's printed in the panel's log when it starts.")
		back(w, r, "/setup")
		return
	}
	pw, pw2 := r.FormValue("password"), r.FormValue("confirm")
	if msg := checkNewPassword(pw, pw2); msg != "" {
		setError(w, msg)
		back(w, r, "/setup")
		return
	}
	hash := auth.HashPassword(pw)
	a.Store.UpdateSettings(func(s *store.Settings) {
		s.PasswordHash = hash
		if n := strings.TrimSpace(r.FormValue("panelName")); n != "" {
			s.PanelName = n
		}
	})
	log.Printf("admin password set from %s", ip)
	a.sessions.Issue(w)
	back(w, r, "/")
}

func checkNewPassword(pw, confirm string) string {
	if len(pw) < minPassword {
		return "Passwords need at least 8 characters."
	}
	if pw != confirm {
		return "The passwords don't match."
	}
	return ""
}

func (a *App) loginPage(w http.ResponseWriter, r *http.Request) {
	if a.Store.Settings().PasswordHash == "" {
		back(w, r, "/setup")
		return
	}
	if a.sessions.Valid(r) {
		back(w, r, "/")
		return
	}
	a.render(w, r, "login", "Sign in", "", nil)
}

func (a *App) loginSubmit(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if err := a.limiter.Check(ip); err != nil {
		setError(w, "Too many wrong passwords. Try again in 15 minutes.")
		back(w, r, "/login")
		return
	}
	if !auth.CheckPassword(a.Store.Settings().PasswordHash, r.FormValue("password")) {
		a.limiter.Fail(ip)
		log.Printf("failed sign-in from %s", ip)
		setError(w, "Wrong password.")
		back(w, r, "/login")
		return
	}
	if a.Store.Settings().TwoFactor() {
		// Failures are only cleared once the code step passes too, so
		// re-entering a known password doesn't buy more code guesses.
		a.sessions.IssuePending(w)
		back(w, r, "/login/code")
		return
	}
	a.limiter.Reset(ip)
	a.sessions.Issue(w)
	back(w, r, "/")
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	a.sessions.Clear(w)
	back(w, r, "/login")
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (a *App) panelSettingsPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "panel_settings", "Panel settings", "settings", nil)
}

func (a *App) savePanelSettings(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("panelName"))
	accent := r.FormValue("accent")
	theme := r.FormValue("theme")
	if name == "" || len(name) > 40 {
		setError(w, "The panel name must be 1 to 40 characters.")
		back(w, r, "/settings")
		return
	}
	if !hexColor.MatchString(accent) {
		accent = "#3b82f6"
	}
	switch theme {
	case "system", "light", "dark":
	default:
		theme = "system"
	}
	a.Store.UpdateSettings(func(s *store.Settings) {
		s.PanelName, s.Accent, s.Theme = name, strings.ToLower(accent), theme
	})
	setFlash(w, "Settings saved.")
	back(w, r, "/settings")
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	if !auth.CheckPassword(a.Store.Settings().PasswordHash, r.FormValue("current")) {
		setError(w, "Your current password is wrong.")
		back(w, r, "/settings")
		return
	}
	pw, pw2 := r.FormValue("password"), r.FormValue("confirm")
	if msg := checkNewPassword(pw, pw2); msg != "" {
		setError(w, msg)
		back(w, r, "/settings")
		return
	}
	hash := auth.HashPassword(pw)
	a.Store.UpdateSettings(func(s *store.Settings) { s.PasswordHash = hash })
	// The old cookie is now invalid; issue a fresh one for this browser.
	a.sessions.Issue(w)
	setFlash(w, "Password changed. Other browsers have been signed out.")
	back(w, r, "/settings")
}
