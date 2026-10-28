package mc

import (
	"testing"
	"time"

	"github.com/Revise0592/McPanel/internal/store"
)

func TestReadyLineRecordsStartOrRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(dir, st)
	done := `[12:00:00 INFO]: Done (3.2s)! For help, type "help"`

	i := newInstance(m, "a")
	i.state = Starting
	i.addLine(done)
	i.addLine(done) // already running: not a second start
	i.state, i.restarting = Starting, true
	i.addLine(done)

	evs := m.Activity().List("a", 0)
	if len(evs) != 2 || evs[1].Text != "Started" || evs[0].Text != "Restarted" {
		t.Fatalf("events = %+v", evs)
	}
	if i.restarting {
		t.Error("restarting was not cleared")
	}
}

func TestPlayerStatsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(dir, st)
	i := newInstance(m, "a")
	i.state = Running
	i.addLine("[09:25:30 INFO]: Steve joined the game")
	i.mu.Lock()
	i.players["Steve"] = i.players["Steve"].Add(-90 * time.Minute) // pretend they joined earlier
	i.mu.Unlock()
	i.addLine("[10:55:30 INFO]: Steve left the game")

	again := newInstance(m, "a") // as after a panel restart
	s := again.PlayerStats()["Steve"]
	if s.Played < 90*time.Minute || s.Played > 91*time.Minute || s.FirstSeen.IsZero() {
		t.Fatalf("stats = %+v", s)
	}
}
