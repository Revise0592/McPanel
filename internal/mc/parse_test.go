package mc

import "testing"

func TestParsePlayers(t *testing.T) {
	i := newInstance(nil, "x")
	i.state = Running
	for _, l := range []string{
		"[09:25:30 INFO]: UUID of player LarveyHong is 114a5ad3-1b19-41f7-b914-06c25e6b7072",
		"[09:25:30 INFO]: System chat: LarveyHong joined the game",
		"[09:25:30 INFO]: LarveyHong[/127.0.0.1:47928] logged in with entity id 120 at ([minecraft:overworld]-664.4, 67.0, 277.4)",
	} {
		i.parseLocked(l)
	}
	if _, ok := i.players["LarveyHong"]; !ok {
		t.Fatalf("player not tracked: %v", i.players)
	}
	i.parseLocked("[09:30:00 INFO]: System chat: LarveyHong left the game")
	if len(i.players) != 0 {
		t.Fatalf("player not removed: %v", i.players)
	}
}

func TestParsePlayersWithColourCodes(t *testing.T) {
	i := newInstance(nil, "x")
	i.addLine("[09:29:01 INFO]: System chat: \x1b[38;2;255;255;85mLarveyHong joined the game\x1b[0m")
	if _, ok := i.players["LarveyHong"]; !ok {
		t.Fatalf("coloured join not tracked: %v", i.players)
	}
	if got := i.lines[len(i.lines)-1]; got != "[09:29:01 INFO]: System chat: LarveyHong joined the game" {
		t.Errorf("stored line not cleaned: %q", got)
	}
	i.addLine("[09:30:00 INFO]: System chat: \x1b[38;2;255;255;85mLarveyHong left the game\x1b[0m")
	if len(i.players) != 0 {
		t.Fatalf("coloured leave not handled: %v", i.players)
	}
	// The login line alone is enough to count someone as online.
	i.addLine("[09:31:00 INFO]: Alex_2[/127.0.0.1:44108] logged in with entity id 107 at ([minecraft:overworld]1.0, 2.0, 3.0)")
	if _, ok := i.players["Alex_2"]; !ok {
		t.Fatalf("login line not tracked: %v", i.players)
	}
}
