package props

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetPreservesLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.properties")
	orig := "#Minecraft server properties\n#Mon Oct 27\nmotd=Hello\ndifficulty=easy\n\npvp=true\n"
	os.WriteFile(path, []byte(orig), 0o644)

	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Set("motd", "Hello") {
		t.Error("setting an unchanged value reported a change")
	}
	f.Set("difficulty", "hard")
	f.Set("max-players", "5")
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "#Minecraft server properties\n#Mon Oct 27\nmotd=Hello\ndifficulty=hard\n\npvp=true\nmax-players=5\n"
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMissingFileAndNewlines(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "nope.properties"))
	if err != nil {
		t.Fatal(err)
	}
	if v := f.GetOr("gamemode", "survival"); v != "survival" {
		t.Errorf("default not used: %q", v)
	}
	f.Set("motd", "line one\nline-two=x")
	if v, _ := f.Get("motd"); v != "line one line-two=x" {
		t.Errorf("newline not stripped: %q", v)
	}
	if v, ok := f.Get("line-two"); ok {
		t.Errorf("injected key appeared: %q", v)
	}
}

func TestEscapeRoundTrip(t *testing.T) {
	for _, s := range []string{
		"plain",
		"§aWelcome §lhome\n§7second line",
		"  centred title",
		`back\slash`,
		"emoji 🎮 and café",
	} {
		enc := Escape(s)
		for _, r := range enc {
			if r > 0x7e {
				t.Errorf("Escape(%q) = %q, not ASCII", s, enc)
			}
		}
		if got := Unescape(enc); got != s {
			t.Errorf("round trip %q: escaped %q, back %q", s, enc, got)
		}
	}
	if got := Unescape(`§aHi\nthere`); got != "§aHi\nthere" {
		t.Errorf("Unescape vanilla value: %q", got)
	}
	if got := Unescape(`ends with \`); got != `ends with \` {
		t.Errorf("trailing backslash: %q", got)
	}
	if got := Unescape(`\u00Zz`); got != "u00Zz" {
		t.Errorf("bad escape: %q", got)
	}
}
