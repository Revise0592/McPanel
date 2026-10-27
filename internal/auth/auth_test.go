package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	h := HashPassword("correct horse")
	if !CheckPassword(h, "correct horse") {
		t.Error("right password rejected")
	}
	if CheckPassword(h, "wrong horse") {
		t.Error("wrong password accepted")
	}
	if CheckPassword("garbage", "x") {
		t.Error("malformed hash accepted")
	}
}

func TestSessionsInvalidatedByPasswordChange(t *testing.T) {
	hash := HashPassword("one")
	s := &Sessions{Secret: func() string { return "secret" }, Fingerprint: func() string { return hash }}

	rec := httptest.NewRecorder()
	s.Issue(rec)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	if !s.Valid(req) {
		t.Fatal("fresh session rejected")
	}
	hash = HashPassword("two")
	if s.Valid(req) {
		t.Fatal("session survived a password change")
	}

	// A pending (password-only) cookie must not work as a session.
	rec = httptest.NewRecorder()
	s.IssuePending(rec)
	pend := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		pend.AddCookie(&http.Cookie{Name: CookieName, Value: c.Value})
	}
	if s.Valid(pend) {
		t.Fatal("pending cookie accepted as a session")
	}

	forged := httptest.NewRequest(http.MethodGet, "/", nil)
	forged.AddCookie(&http.Cookie{Name: CookieName, Value: "99999999999.abc"})
	if s.Valid(forged) {
		t.Fatal("forged session accepted")
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter()
	for i := 0; i < maxFailures; i++ {
		if err := l.Check("1.2.3.4"); err != nil {
			t.Fatalf("locked too early at %d", i)
		}
		l.Fail("1.2.3.4")
	}
	if l.Check("1.2.3.4") == nil {
		t.Fatal("not locked after max failures")
	}
	if l.Check("5.6.7.8") != nil {
		t.Fatal("other address locked")
	}
}
