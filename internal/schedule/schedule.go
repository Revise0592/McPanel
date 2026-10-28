// Package schedule runs each server's timed tasks: restarts and console
// commands, daily at a set time or every few hours.
package schedule

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/store"
)

// warnings are how long before a restart players are told about it.
var warnings = []time.Duration{5 * time.Minute, time.Minute}

// ParseAt reads a time of day written as "HH:MM".
func ParseAt(at string) (hour, minute int, ok bool) {
	t, err := time.Parse("15:04", at)
	if err != nil {
		return 0, 0, false
	}
	return t.Hour(), t.Minute(), true
}

// Next is when a task is next due after the given time.
func Next(t store.Task, after time.Time) time.Time {
	if t.EveryHours > 0 {
		return after.Add(time.Duration(t.EveryHours) * time.Hour)
	}
	h, m, ok := ParseAt(t.At)
	if !ok {
		return time.Time{}
	}
	y, mo, d := after.Date()
	next := time.Date(y, mo, d, h, m, 0, 0, after.Location())
	if !next.After(after) {
		next = time.Date(y, mo, d+1, h, m, 0, 0, after.Location())
	}
	return next
}

type state struct {
	after  time.Time // the task is due at Next(task, after)
	warned int       // how many of warnings have been sent
}

// Scheduler runs tasks. It is safe for concurrent use.
type Scheduler struct {
	st  *store.Store
	mgr *mc.Manager

	mu    sync.Mutex
	start time.Time
	tasks map[string]*state
}

// New returns a scheduler. Tasks count from now, so ones missed while the
// panel was off don't all run at once when it starts.
func New(st *store.Store, mgr *mc.Manager) *Scheduler {
	return &Scheduler{st: st, mgr: mgr, start: time.Now(), tasks: map[string]*state{}}
}

func (s *Scheduler) stateLocked(id string) *state {
	ts, ok := s.tasks[id]
	if !ok {
		ts = &state{after: s.start}
		s.tasks[id] = ts
	}
	return ts
}

// Added starts a new task's clock now rather than from when the panel
// started.
func (s *Scheduler) Added(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[id] = &state{after: time.Now()}
}

// NextRun is when a task will next run.
func (s *Scheduler) NextRun(t store.Task) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Next(t, s.stateLocked(t.ID).after)
}

// Run checks the tasks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		s.runDue(time.Now())
	}
}

func (s *Scheduler) runDue(now time.Time) {
	seen := map[string]bool{}
	for _, sv := range s.st.Servers() {
		for _, t := range sv.Tasks {
			seen[t.ID] = true
			if !sv.Installed {
				continue
			}
			inst, err := s.mgr.Get(sv.ID)
			if err != nil {
				continue
			}
			s.check(inst, t, now)
		}
	}
	s.mu.Lock()
	for id := range s.tasks {
		if !seen[id] {
			delete(s.tasks, id)
		}
	}
	s.mu.Unlock()
}

func (s *Scheduler) check(inst *mc.Instance, t store.Task, now time.Time) {
	s.mu.Lock()
	ts := s.stateLocked(t.ID)
	next := Next(t, ts.after)
	if next.IsZero() {
		s.mu.Unlock()
		return
	}
	st := inst.Status()
	running := st.State == mc.Running

	if now.Before(next) {
		if t.Action != store.TaskRestart || !running {
			s.mu.Unlock()
			return
		}
		// Warn once per threshold crossed, saying how long is really left.
		left := next.Sub(now)
		send := false
		for ts.warned < len(warnings) && left <= warnings[ts.warned] {
			ts.warned++
			send = true
		}
		s.mu.Unlock()
		if send {
			msg := "say The server will restart in 1 minute."
			if mins := int(math.Ceil(left.Minutes())); mins > 1 {
				msg = fmt.Sprintf("say The server will restart in %d minutes.", mins)
			}
			inst.Send(msg)
		}
		return
	}

	// Due. Wait for a backup or install to finish before restarting.
	if t.Action == store.TaskRestart && st.Task != "" {
		s.mu.Unlock()
		return
	}
	ts.after, ts.warned = next, 0
	// If the panel was asleep or busy, don't try to catch up on missed runs.
	if Next(t, next).Before(now) {
		ts.after = now
	}
	s.mu.Unlock()

	if !running {
		return
	}
	switch t.Action {
	case store.TaskRestart:
		inst.Record(activity.Info, "Scheduled restart")
		inst.Restart()
	case store.TaskCommand:
		cmd := strings.TrimPrefix(strings.TrimSpace(t.Command), "/")
		if err := inst.Send(cmd); err == nil {
			inst.Logf("> %s (scheduled)", cmd)
		}
	}
}
