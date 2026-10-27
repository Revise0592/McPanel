package auth

import (
	"testing"
	"time"
)

// RFC 6238 appendix B test vector for SHA-1 (8 digits there; the last 6
// digits are what a 6-digit generator produces).
func TestTOTPRFCVector(t *testing.T) {
	key := []byte("12345678901234567890")
	cases := map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"}
	for ts, want := range cases {
		if got := totpCode(key, ts/totpPeriod); got != want {
			t.Errorf("t=%d: got %s, want %s", ts, got, want)
		}
	}
}

func TestCheckTOTP(t *testing.T) {
	secret := NewTOTPSecret()
	key, _ := b32.DecodeString(secret)
	now := time.Unix(1_800_000_000, 0)
	step := now.Unix() / totpPeriod
	code := totpCode(key, step)

	got, ok := CheckTOTP(secret, code, now, 0)
	if !ok || got != step {
		t.Fatal("current code rejected")
	}
	if _, ok := CheckTOTP(secret, code, now, step); ok {
		t.Fatal("replayed code accepted")
	}
	if _, ok := CheckTOTP(secret, totpCode(key, step-1), now, 0); !ok {
		t.Fatal("code from the previous step rejected")
	}
	if _, ok := CheckTOTP(secret, totpCode(key, step-3), now, 0); ok {
		t.Fatal("stale code accepted")
	}
	if _, ok := CheckTOTP(secret, "12345", now, 0); ok {
		t.Fatal("short code accepted")
	}
	if _, ok := CheckTOTP(secret, code[:3]+" "+code[3:], now, 0); !ok {
		t.Fatal("code with a space rejected")
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, hashes := NewRecoveryCodes(10)
	if len(codes) != 10 || len(hashes) != 10 {
		t.Fatal("wrong count")
	}
	c := codes[0]
	if HashRecoveryCode(" "+c[:5]+c[6:]+" ") != hashes[0] || HashRecoveryCode(c) != hashes[0] {
		t.Error("normalised code hashes differently")
	}
	if !LooksLikeRecoveryCode(c) || LooksLikeRecoveryCode("123456") {
		t.Error("recovery code detection wrong")
	}
}

func TestTOTPURI(t *testing.T) {
	got := TOTPURI("ABC", "My Panel", "admin")
	if got != "otpauth://totp/My%20Panel:admin?issuer=My+Panel&secret=ABC" {
		t.Errorf("got %s", got)
	}
}
