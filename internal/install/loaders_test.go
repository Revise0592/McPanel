package install

import "testing"

func TestNeoForgeMC(t *testing.T) {
	for _, c := range []struct {
		v, mc string
		beta  bool
		ok    bool
	}{
		{"21.1.77", "1.21.1", false, true},
		{"21.0.167", "1.21", false, true},
		{"20.4.237", "1.20.4", false, true},
		{"20.2.3-beta", "1.20.2", true, true},
		{"26.1.2.121", "26.1.2", false, true},
		{"26.3.0.84", "26.3", false, true},
		{"26.3.0.71-beta", "26.3", true, true},
		{"0.25w14craftmine.3", "", false, false},
		{"47.1.3", "", false, false},
	} {
		mc, beta, ok := neoForgeMC(c.v)
		if mc != c.mc || beta != c.beta || ok != c.ok {
			t.Errorf("%s: got %q %v %v, want %q %v %v", c.v, mc, beta, ok, c.mc, c.beta, c.ok)
		}
	}
}

func TestNewerNeoForge(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"21.1.10", "", true},
		{"21.1.10", "21.1.9", true},
		{"21.1.9", "21.1.10", false},
		{"21.1.1", "21.1.50-beta", true},
		{"21.1.50-beta", "21.1.1", false},
		{"26.3.0.75-beta", "26.3.0.74-beta", true},
	} {
		if got := newerNeoForge(c.a, c.b); got != c.want {
			t.Errorf("newerNeoForge(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
