package web

import (
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"slices"
	"time"

	"rsc.io/qr"

	"github.com/Revise0592/McPanel/internal/auth"
	"github.com/Revise0592/McPanel/internal/store"
)

const recoveryCodeCount = 10

// checkSecondFactor verifies a TOTP or recovery code against the current
// settings, consuming it on success. It reports whether it matched and
// whether a recovery code was used.
func (a *App) checkSecondFactor(code string) (ok, recovery bool) {
	set := a.Store.Settings()
	if !set.TwoFactor() {
		return false, false
	}
	if auth.LooksLikeRecoveryCode(code) {
		h := auth.HashRecoveryCode(code)
		if !slices.Contains(set.RecoveryHashes, h) {
			return false, true
		}
		a.Store.UpdateSettings(func(s *store.Settings) {
			s.RecoveryHashes = slices.DeleteFunc(s.RecoveryHashes, func(x string) bool { return x == h })
		})
		return true, true
	}
	step, ok := auth.CheckTOTP(set.TOTPSecret, code, time.Now(), set.TOTPLastStep)
	if !ok {
		return false, false
	}
	a.Store.UpdateSettings(func(s *store.Settings) { s.TOTPLastStep = step })
	return true, false
}

func (a *App) loginCodePage(w http.ResponseWriter, r *http.Request) {
	if !a.sessions.ValidPending(r) {
		back(w, r, "/login")
		return
	}
	a.render(w, r, "login_2fa", "Sign in", "", nil)
}

func (a *App) loginCodeSubmit(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.sessions.ValidPending(r) {
		setError(w, "That took too long. Please sign in again.")
		back(w, r, "/login")
		return
	}
	if err := a.limiter.Check(ip); err != nil {
		setError(w, "Too many wrong codes. Try again in 15 minutes.")
		back(w, r, "/login/code")
		return
	}
	ok, recovery := a.checkSecondFactor(r.FormValue("code"))
	if !ok {
		a.limiter.Fail(ip)
		log.Printf("failed two-factor code from %s", ip)
		setError(w, "That code isn't right.")
		back(w, r, "/login/code")
		return
	}
	a.limiter.Reset(ip)
	a.sessions.Issue(w)
	if recovery {
		left := len(a.Store.Settings().RecoveryHashes)
		log.Printf("recovery code used from %s (%d left)", ip, left)
		setFlash(w, fmt.Sprintf("You used a recovery code; %d left. You can make new ones in Settings.", left))
	}
	back(w, r, "/")
}

type twoFactorSetup struct {
	Secret string
	QR     template.URL
}

func (a *App) twoFactorSetupPage(w http.ResponseWriter, r *http.Request) {
	set := a.Store.Settings()
	if set.TwoFactor() {
		back(w, r, "/settings")
		return
	}
	secret := set.TOTPPending
	if secret == "" {
		secret = auth.NewTOTPSecret()
		a.Store.UpdateSettings(func(s *store.Settings) { s.TOTPPending = secret })
	}
	uri := auth.TOTPURI(secret, set.PanelName, set.Username)
	code, err := qr.Encode(uri, qr.M)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	code.Scale = 6
	d := twoFactorSetup{
		Secret: secret,
		// Built from our own PNG bytes, so safe to mark as a URL.
		QR: template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG())),
	}
	a.render(w, r, "twofactor_setup", "Two-factor sign-in", "settings", d)
}

func (a *App) twoFactorSetupSubmit(w http.ResponseWriter, r *http.Request) {
	set := a.Store.Settings()
	if set.TwoFactor() || set.TOTPPending == "" {
		back(w, r, "/settings")
		return
	}
	step, ok := auth.CheckTOTP(set.TOTPPending, r.FormValue("code"), time.Now(), 0)
	if !ok {
		setError(w, "That code isn't right. Check your phone's clock is correct and try the newest code.")
		back(w, r, "/settings/2fa")
		return
	}
	codes, hashes := auth.NewRecoveryCodes(recoveryCodeCount)
	a.Store.UpdateSettings(func(s *store.Settings) {
		s.TOTPSecret, s.TOTPPending, s.TOTPLastStep, s.RecoveryHashes = s.TOTPPending, "", step, hashes
	})
	log.Printf("two-factor sign-in turned on from %s", clientIP(r))
	// The session fingerprint changed; keep this browser signed in.
	a.sessions.Issue(w)
	a.render(w, r, "twofactor_codes", "Recovery codes", "settings", recoveryData{Codes: codes, JustEnabled: true})
}

type recoveryData struct {
	Codes       []string
	JustEnabled bool
}

func (a *App) twoFactorDisable(w http.ResponseWriter, r *http.Request) {
	set := a.Store.Settings()
	if !set.TwoFactor() {
		back(w, r, "/settings")
		return
	}
	if !auth.CheckPassword(set.PasswordHash, r.FormValue("password")) {
		setError(w, "Your password is wrong.")
		back(w, r, "/settings")
		return
	}
	if ok, _ := a.checkSecondFactor(r.FormValue("code")); !ok {
		setError(w, "That code isn't right.")
		back(w, r, "/settings")
		return
	}
	a.Store.UpdateSettings(func(s *store.Settings) { s.DisableTwoFactor() })
	log.Printf("two-factor sign-in turned off from %s", clientIP(r))
	a.sessions.Issue(w)
	setFlash(w, "Two-factor sign-in is off. Other browsers have been signed out.")
	back(w, r, "/settings")
}

func (a *App) twoFactorNewCodes(w http.ResponseWriter, r *http.Request) {
	if !a.Store.Settings().TwoFactor() {
		back(w, r, "/settings")
		return
	}
	if ok, _ := a.checkSecondFactor(r.FormValue("code")); !ok {
		setError(w, "That code isn't right.")
		back(w, r, "/settings")
		return
	}
	codes, hashes := auth.NewRecoveryCodes(recoveryCodeCount)
	a.Store.UpdateSettings(func(s *store.Settings) { s.RecoveryHashes = hashes })
	a.render(w, r, "twofactor_codes", "Recovery codes", "settings", recoveryData{Codes: codes})
}
