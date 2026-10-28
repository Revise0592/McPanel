package activity

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestAddFoldsRepeats(t *testing.T) {
	l := New(t.TempDir())
	var calls atomic.Int32
	done := make(chan struct{}, 10)
	l.OnEvent(func(string, Event) { calls.Add(1); done <- struct{}{} })

	now := time.Now()
	l.Add("s", Event{Time: now, Kind: Failed, Text: "Backup failed"})
	l.Add("s", Event{Time: now.Add(time.Minute), Kind: Failed, Text: "Backup failed"})
	l.Add("s", Event{Time: now.Add(2 * time.Minute), Text: "Started"})
	l.Add("s", Event{Time: now.Add(2 * time.Hour), Kind: Failed, Text: "Backup failed"})
	for range 3 {
		<-done
	}

	evs := l.List("s", 0)
	if len(evs) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(evs), evs)
	}
	if evs[2].Repeats != 1 || evs[0].Text != "Backup failed" || evs[1].Text != "Started" {
		t.Errorf("unexpected events: %+v", evs)
	}
	if calls.Load() != 3 {
		t.Errorf("hook ran %d times, want 3", calls.Load())
	}
	if got := l.List("s", 1); len(got) != 1 || !got[0].Time.Equal(evs[0].Time) {
		t.Errorf("List(1) = %+v", got)
	}
}

func TestSeenAndKeep(t *testing.T) {
	l := New(t.TempDir())
	l.Add("s", Event{Kind: Available, Text: "Build 5 is out", Keys: []string{"build:5"}})
	if !l.Seen("s", "build:5") || l.Seen("s", "build:6") || l.Seen("other", "build:5") {
		t.Error("Seen gave the wrong answer")
	}
	for i := range keep + 10 {
		l.Add("s", Event{Text: string(rune('a' + i%26)), Time: time.Now().Add(time.Duration(i) * time.Second)})
	}
	if n := len(l.List("s", 0)); n != keep {
		t.Errorf("kept %d events, want %d", n, keep)
	}
	l.Remove("s")
	if len(l.List("s", 0)) != 0 {
		t.Error("Remove left events behind")
	}
}
