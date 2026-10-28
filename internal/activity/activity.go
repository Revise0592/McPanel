// Package activity keeps a short history of what happened to each server,
// such as backups, updates and crashes, and hands each new event to an
// optional hook for alerts.
package activity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// Kind sorts events for display and alerts.
type Kind string

// Event kinds. Plain events (backup saved, installed) have no kind.
const (
	Info      Kind = ""
	Crash     Kind = "crash"
	Failed    Kind = "failed"
	Available Kind = "update" // a newer server build or add-on is out
	Power     Kind = "power"  // started, stopped or restarted
)

// Event is one entry in a server's history.
type Event struct {
	Time time.Time `json:"time"`
	Kind Kind      `json:"kind,omitempty"`
	Text string    `json:"text"`
	// Keys identify what the event was about, so the same thing isn't
	// reported twice (see Seen).
	Keys []string `json:"keys,omitempty"`
	// Repeats counts identical events folded into this one.
	Repeats int `json:"repeats,omitempty"`
}

const (
	keep = 200
	// An event matching the previous one within this window is counted
	// on it instead of being added, so a failure retried every minute
	// doesn't flood the list or the alerts.
	repeatWindow = time.Hour
)

// Log stores events under dataDir/activity, one file per server. It is safe
// for concurrent use.
type Log struct {
	dir string

	mu   sync.Mutex
	hook func(serverID string, e Event)
}

// New returns a log stored under dataDir.
func New(dataDir string) *Log { return &Log{dir: filepath.Join(dataDir, "activity")} }

// OnEvent sets a function run, in its own goroutine, for each new event.
func (l *Log) OnEvent(fn func(serverID string, e Event)) {
	l.mu.Lock()
	l.hook = fn
	l.mu.Unlock()
}

func (l *Log) path(id string) string { return filepath.Join(l.dir, id+".json") }

func (l *Log) readLocked(id string) []Event {
	var evs []Event
	if b, err := os.ReadFile(l.path(id)); err == nil {
		json.Unmarshal(b, &evs)
	}
	return evs
}

func (l *Log) writeLocked(id string, evs []Event) error {
	if err := os.MkdirAll(l.dir, 0o750); err != nil {
		return err
	}
	b, err := json.Marshal(evs)
	if err != nil {
		return err
	}
	tmp := l.path(id) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, l.path(id))
}

// Add records an event for a server. Time defaults to now.
func (l *Log) Add(id string, e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	l.mu.Lock()
	evs := l.readLocked(id)
	if n := len(evs); n > 0 {
		if last := &evs[n-1]; last.Kind == e.Kind && last.Text == e.Text && e.Time.Sub(last.Time) < repeatWindow {
			last.Repeats++
			l.writeLocked(id, evs)
			l.mu.Unlock()
			return
		}
	}
	evs = append(evs, e)
	if len(evs) > keep {
		evs = slices.Clone(evs[len(evs)-keep:])
	}
	l.writeLocked(id, evs)
	hook := l.hook
	l.mu.Unlock()
	if hook != nil {
		go hook(id, e)
	}
}

// List returns up to n of a server's events, newest first. n <= 0 means all.
func (l *Log) List(id string, n int) []Event {
	l.mu.Lock()
	evs := l.readLocked(id)
	l.mu.Unlock()
	slices.Reverse(evs)
	if n > 0 && len(evs) > n {
		evs = evs[:n]
	}
	return evs
}

// Seen reports whether any stored event for the server carries key.
func (l *Log) Seen(id, key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.readLocked(id) {
		if slices.Contains(e.Keys, key) {
			return true
		}
	}
	return false
}

// Remove deletes a server's history.
func (l *Log) Remove(id string) {
	l.mu.Lock()
	os.Remove(l.path(id))
	l.mu.Unlock()
}
