package players

import (
	"testing"

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
