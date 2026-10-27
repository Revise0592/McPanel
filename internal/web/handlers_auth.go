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
	user := strings.TrimSpace(r.FormValue("username"))
	if msg := checkUsername(user); msg != "" {
		setError(w, msg)
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
		s.Username = user
		s.PasswordHash = hash
		if n := strings.TrimSpace(r.FormValue("panelName")); n != "" {
			s.PanelName = n
		}
	})
	log.Printf("admin account %q created from %s", user, ip)
	a.sessions.Issue(w)
	back(w, r, "/")
}

var validUsername = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

func checkUsername(u string) string {
	if !validUsername.MatchString(u) {
		return "Usernames are 1 to 32 letters, numbers, dots, dashes or underscores."
	}
	return ""
}

// usernameMatches compares case-insensitively and in constant time.
func usernameMatches(want, got string) bool {
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(want)), []byte(strings.ToLower(strings.TrimSpace(got)))) == 1
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
	// HTTPSOnly warns that sign-in will silently fail on this plain HTTP page.
	httpsOnly := a.Store.Settings().BehindHTTPSProxy && !viaHTTPS(r)
	a.render(w, r, "login", "Sign in", "", struct{ HTTPSOnly bool }{httpsOnly})
}

func (a *App) loginSubmit(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if err := a.limiter.Check(ip); err != nil {
		setError(w, "Too many failed sign-ins. Try again in 15 minutes.")
		back(w, r, "/login")
		return
	}
	set := a.Store.Settings()
	// Check the password even when the username is wrong, so timing doesn't
	// reveal which one was right.
	pwOK := auth.CheckPassword(set.PasswordHash, r.FormValue("password"))
	if !usernameMatches(set.Username, r.FormValue("username")) || !pwOK {
		a.limiter.Fail(ip)
		log.Printf("failed sign-in from %s", ip)
		setError(w, "Wrong username or password.")
		back(w, r, "/login")
		return
	}
	if set.TwoFactor() {
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

// Panel settings are split into sections, each its own page.
const (
	appearanceURL = "/settings"
	accountURL    = "/settings/account"
	securityURL   = "/settings/security"
)

type panelSettingsData struct {
	Section string
	HTTPS   bool // this page was loaded over HTTPS
	Accents []string
}

// accentPresets are offered as one-click swatches next to the colour picker.
var accentPresets = []string{"#3b82f6", "#0d9488", "#16a34a", "#ca8a04", "#ea580c", "#dc2626", "#db2777", "#7c3aed", "#475569"}

func (a *App) panelSettingsPage(section string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := panelSettingsData{Section: section, HTTPS: viaHTTPS(r), Accents: accentPresets}
		a.render(w, r, "panel_settings", "Settings", "settings", d)
	}
}

func (a *App) saveHTTPSProxy(w http.ResponseWriter, r *http.Request) {
	on := r.FormValue("behindHTTPSProxy") == "on"
	// Browsers drop Secure cookies sent over plain HTTP, so turning this on
	// from an HTTP page would sign the user out for good.
	if on && !viaHTTPS(r) {
		setError(w, "Open McPanel through your HTTPS address first, then turn this on. Turning it on over plain HTTP would lock you out.")
		back(w, r, securityURL)
		return
	}
	a.Store.UpdateSettings(func(s *store.Settings) { s.BehindHTTPSProxy = on })
	// Re-issue the session so this browser's cookie picks up the new flag.
	a.sessions.Issue(w)
	if on {
		setFlash(w, "Saved. Sign-in now only works over HTTPS.")
	} else {
		setFlash(w, "Saved.")
	}
	back(w, r, securityURL)
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

func (a *App) changeUsername(w http.ResponseWriter, r *http.Request) {
	if !auth.CheckPassword(a.Store.Settings().PasswordHash, r.FormValue("current")) {
		setError(w, "Your current password is wrong.")
		back(w, r, accountURL)
		return
	}
	user := strings.TrimSpace(r.FormValue("username"))
	if msg := checkUsername(user); msg != "" {
		setError(w, msg)
		back(w, r, accountURL)
		return
	}
	a.Store.UpdateSettings(func(s *store.Settings) { s.Username = user })
	setFlash(w, "Username changed. Use it the next time you sign in.")
	back(w, r, accountURL)
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	if !auth.CheckPassword(a.Store.Settings().PasswordHash, r.FormValue("current")) {
		setError(w, "Your current password is wrong.")
		back(w, r, accountURL)
		return
	}
	pw, pw2 := r.FormValue("password"), r.FormValue("confirm")
	if msg := checkNewPassword(pw, pw2); msg != "" {
		setError(w, msg)
		back(w, r, accountURL)
		return
	}
	hash := auth.HashPassword(pw)
	a.Store.UpdateSettings(func(s *store.Settings) { s.PasswordHash = hash })
	// The old cookie is now invalid; issue a fresh one for this browser.
	a.sessions.Issue(w)
	setFlash(w, "Password changed. Other browsers have been signed out.")
	back(w, r, accountURL)
}
