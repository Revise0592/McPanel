package web

import "testing"

func TestValidJoinAddress(t *testing.T) {
	for s, want := range map[string]bool{
		"mc.mcraid.uk:19134":   true,
		"mc.mcraid.uk":         true,
		"203.0.113.5:25565":    true,
		"[2001:db8::1]:25565":  true,
		"2001:db8::1":          true,
		"mc.mcraid.uk:0":       false,
		"mc.mcraid.uk:70000":   false,
		"mc.mcraid.uk:abc":     false,
		"https://mc.mcraid.uk": false,
		"mc.mcraid.uk/join":    false,
		"mc mcraid uk":         false,
		":25565":               false,
	} {
		if got := validJoinAddress(s); got != want {
			t.Errorf("validJoinAddress(%q) = %v, want %v", s, got, want)
		}
	}
}
