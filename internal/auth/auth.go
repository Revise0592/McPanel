// Package auth handles the admin password, signed session cookies and
// sign-in rate limiting.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	CookieName     = "mcpanel_session"
	SessionLength  = 30 * 24 * time.Hour
	maxFailures    = 10
	failureWindow  = 15 * time.Minute
	argonTime      = 2
	argonMemoryKiB = 64 * 1024
	argonThreads   = 2
	argonKeyLen    = 32
)

// HashPassword returns an encoded argon2id hash.
func HashPassword(pw string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemoryKiB, argonTime, argonThreads, enc.EncodeToString(salt), enc.EncodeToString(key))
}

// CheckPassword verifies pw against an encoded hash.
func CheckPassword(encoded, pw string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// Sessions issues and verifies stateless signed cookies. The signature
// covers Fingerprint (the password hash and 2FA secret), so changing the
// password or turning 2FA on or off signs out every browser.
type Sessions struct {
	Secret      func() string
	Fingerprint func() string
	Secure      func() bool // nil means never Secure
}

// PendingCookie marks a browser that has passed the password step and
// still needs a two-factor code.
const (
	PendingCookie = "mcpanel_2fa"
	pendingLength = 5 * time.Minute
)

func (s *Sessions) sign(kind string, expiry int64) string {
	mac := hmac.New(sha256.New, []byte(s.Secret()))
	fmt.Fprintf(mac, "%s|%d|%s", kind, expiry, s.Fingerprint())
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Sessions) set(w http.ResponseWriter, name, kind string, d time.Duration) {
	exp := time.Now().Add(d).Unix()
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    strconv.FormatInt(exp, 10) + "." + s.sign(kind, exp),
		Path:     "/",
		Expires:  time.Unix(exp, 0),
		HttpOnly: true,
		Secure:   s.Secure != nil && s.Secure(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Sessions) check(r *http.Request, name, kind string) bool {
	if s.Fingerprint() == "" {
		return false
	}
	c, err := r.Cookie(name)
	if err != nil {
		return false
	}
	expStr, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(s.sign(kind, exp)))
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

// Issue sets a new session cookie.
func (s *Sessions) Issue(w http.ResponseWriter) {
	clearCookie(w, PendingCookie)
	s.set(w, CookieName, "session", SessionLength)
}

// Clear removes the session cookie.
func (s *Sessions) Clear(w http.ResponseWriter) {
	clearCookie(w, CookieName)
	clearCookie(w, PendingCookie)
}

// Valid reports whether the request carries a valid session.
func (s *Sessions) Valid(r *http.Request) bool { return s.check(r, CookieName, "session") }

// IssuePending records that the password step passed.
func (s *Sessions) IssuePending(w http.ResponseWriter) {
	s.set(w, PendingCookie, "pending", pendingLength)
}

// ValidPending reports whether the browser passed the password step recently.
func (s *Sessions) ValidPending(r *http.Request) bool { return s.check(r, PendingCookie, "pending") }

// ErrLocked is returned while an address is locked out.
var ErrLocked = errors.New("too many failed attempts, try again in a few minutes")

// Limiter tracks failed sign-ins per address.
type Limiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

func NewLimiter() *Limiter { return &Limiter{failures: map[string][]time.Time{}} }

func (l *Limiter) recent(addr string) []time.Time {
	cut := time.Now().Add(-failureWindow)
	var keep []time.Time
	for _, t := range l.failures[addr] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	l.failures[addr] = keep
	return keep
}

// Check returns ErrLocked if addr has too many recent failures.
func (l *Limiter) Check(addr string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.recent(addr)) >= maxFailures {
		return ErrLocked
	}
	return nil
}

// Fail records a failed attempt.
func (l *Limiter) Fail(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[addr] = append(l.recent(addr), time.Now())
}

// Reset clears failures after a successful sign-in.
func (l *Limiter) Reset(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, addr)
}
