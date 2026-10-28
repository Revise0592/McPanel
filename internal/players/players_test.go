package players

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Revise0592/McPanel/internal/store"
)

func TestOfflineUUID(t *testing.T) {
	// Matches Java's UUID.nameUUIDFromBytes("OfflinePlayer:Notch").
	if got := offlineUUID("Notch"); got != "b50ad385-829d-3141-a216-7e7d7539ba7f" {
		t.Errorf("got %s", got)
	}
}

func TestValidName(t *testing.T) {
	cases := []struct {
		typ, name string
		ok        bool
	}{
		{store.TypePaper, "Steve_01", true},
		{store.TypePaper, "has space", false},
		{store.TypePaper, "waytoolongname12345", false},
		{store.TypeBedrock, "Steve Two", true},
		{store.TypeBedrock, `bad"quote`, false},
	}
	for _, c := range cases {
		if got := ValidName(c.typ, c.name); got != c.ok {
			t.Errorf("ValidName(%s, %q) = %v", c.typ, c.name, got)
		}
	}
}

func TestCommandQuoting(t *testing.T) {
	if c := command(store.TypeBedrock, Whitelist, "Steve Two", true); c != `allowlist add "Steve Two"` {
		t.Errorf("got %q", c)
	}
	if c := command(store.TypePaper, Bans, "Steve", false); c != "pardon Steve" {
		t.Errorf("got %q", c)
	}
}

func TestRecentPlayers(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "usercache.json"), []byte(`[
		{"name":"Alex","uuid":"x","expiresOn":"2026-11-20 10:00:00 +0000"},
		{"name":"Steve","uuid":"y","expiresOn":"2026-11-25 10:00:00 +0000"},
		{"name":"Online","uuid":"z","expiresOn":"2026-11-26 10:00:00 +0000"}]`), 0o644)
	seen := map[string]time.Time{"Alex": time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)}
	got := RecentPlayers(store.Server{Type: store.TypePaper}, dir, seen, []string{"Online"}, 10)
	if len(got) != 2 || got[0].Name != "Alex" || got[1].Name != "Steve" {
		t.Fatalf("RecentPlayers = %+v", got)
	}
	if want := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC); !got[1].Seen.Equal(want) {
		t.Errorf("Steve seen %v, want %v", got[1].Seen, want)
	}
}
