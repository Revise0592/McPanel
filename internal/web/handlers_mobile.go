package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"rsc.io/qr"

	"github.com/Revise0592/McPanel/internal/store"
)

const (
	mobileURL = "/settings/mobile"
	// pairingFor is how long a pairing code works.
	pairingFor = 10 * time.Minute
	// pairingAlphabet leaves out letters and digits that look alike, since
	// the code may be typed in by hand.
	pairingAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	pairingLen      = 8
)

// pairing holds the one pairing code that can be used right now. It lives
// in memory only, so restarting the panel cancels it.
type pairing struct {
	mu      sync.Mutex
	code    string
	expires time.Time
}

// newCode replaces any current code with a fresh one.
func (p *pairing) newCode() (string, time.Time) {
	b := make([]byte, pairingLen)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		// 256 isn't a multiple of the alphabet's length, so this leans very
		// slightly towards the first letters. It's still about 39 bits.
		b[i] = pairingAlphabet[int(b[i])%len(pairingAlphabet)]
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.code, p.expires = string(b), time.Now().Add(pairingFor)
	return p.code, p.expires
}

// current returns the code still waiting to be used, if any.
func (p *pairing) current() (string, time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.code == "" || time.Now().After(p.expires) {
		return "", time.Time{}, false
	}
	return p.code, p.expires, true
}

func (p *pairing) cancel() {
	p.mu.Lock()
	p.code = ""
	p.mu.Unlock()
}

// use reports whether code is the current one, and if so uses it up.
func (p *pairing) use(code string) bool {
	code = normalizeCode(code)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.code == "" || time.Now().After(p.expires) || len(code) != len(p.code) ||
		subtle.ConstantTimeCompare([]byte(code), []byte(p.code)) != 1 {
		return false
	}
	p.code = ""
	return true
}

// normalizeCode accepts codes typed with spaces, dashes or in lower case.
func normalizeCode(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '-' || r == ' ':
			return -1
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		}
		return r
	}, s)
}

// groupCode shows a code as ABCD-EFGH, easier to read out and type.
func groupCode(c string) string {
	if len(c) != pairingLen {
		return c
	}
	return c[:4] + "-" + c[4:]
}

type mobileData struct {
	panelSettingsData
	Devices []store.Device
	// Set while a pairing code is waiting to be used.
	Code    string
	Expires time.Time
	QR      template.URL
	URL     string // the panel address the app connects to
}

// panelBaseURL is the address this browser reached the panel at, which is
// what the phone is told to connect to.
func panelBaseURL(r *http.Request) string {
	scheme := "http"
	if viaHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// pairingLink is what the QR code holds: the app opens it and gets both the
// panel's address and the code.
func pairingLink(base, code string) string {
	q := url.Values{"url": {base}, "code": {code}}
	return "mcpanel://pair?" + q.Encode()
}

func (a *App) mobilePage(w http.ResponseWriter, r *http.Request) {
	d := mobileData{
		panelSettingsData: panelSettingsData{Section: "mobile", HTTPS: viaHTTPS(r), Accents: accentPresets},
		Devices:           a.Store.Devices(),
		URL:               panelBaseURL(r),
	}
	if code, exp, ok := a.pairing.current(); ok {
		png, err := qr.Encode(pairingLink(d.URL, code), qr.M)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		png.Scale = 6
		d.Code, d.Expires = groupCode(code), exp
		// Built from our own PNG bytes, so safe to mark as a URL.
		d.QR = template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png.PNG()))
	}
	a.render(w, r, "panel_settings", "Settings", "settings", d)
}

func (a *App) startPairing(w http.ResponseWriter, r *http.Request) {
	a.pairing.newCode()
	back(w, r, mobileURL)
}

func (a *App) cancelPairing(w http.ResponseWriter, r *http.Request) {
	a.pairing.cancel()
	back(w, r, mobileURL)
}

func (a *App) removeDevice(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.RemoveDevice(r.PathValue("id")); err != nil {
		setError(w, "That device was already removed.")
	} else {
		setFlash(w, "Device removed. It can no longer reach the panel.")
	}
	back(w, r, mobileURL)
}
