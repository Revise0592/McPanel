package mc

import (
	"context"
	"io"
	"runtime"
	"slices"
	"time"
)

// Usage history for the charts: one sample a minute for the last day,
// kept in memory.
const (
	SampleEvery = time.Minute
	historyKeep = 24 * time.Hour
)

// Sample is one point of usage history.
type Sample struct {
	At       time.Time
	CPU      float64 // percent of the machine
	MemoryMB int
	TPS      float64 // 0 when unknown
}

// RunSampler records usage for every running server until ctx is cancelled.
func (m *Manager) RunSampler(ctx context.Context) {
	t := time.NewTicker(SampleEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			m.mu.Lock()
			all := make([]*Instance, 0, len(m.insts))
			for _, i := range m.insts {
				all = append(all, i)
			}
			m.mu.Unlock()
			for _, i := range all {
				i.sample(now)
			}
		}
	}
}

// sample adds a history point if the server is running. CPU is averaged
// since the previous sample, so the first one after a start is skipped.
func (i *Instance) sample(now time.Time) {
	i.mu.Lock()
	if i.cmd == nil || i.cmd.Process == nil || i.state != Running {
		i.histPrev = cpuSample{}
		i.mu.Unlock()
		return
	}
	pid := i.cmd.Process.Pid
	i.mu.Unlock()

	ticks, mem, ok := readProc(pid)
	if !ok {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	prev := i.histPrev
	i.histPrev = cpuSample{ticks, now}
	if prev.at.IsZero() || ticks < prev.ticks {
		return
	}
	secs := float64(ticks-prev.ticks) / clockTicks
	s := Sample{At: now, CPU: min(100, 100*secs/now.Sub(prev.at).Seconds()/float64(runtime.NumCPU())), MemoryMB: mem}
	if i.paper {
		if time.Since(i.tpsAt) < tpsEvery+5*time.Second {
			s.TPS = i.tps
		} else if i.stdin != nil && time.Since(i.tpsAsked) > tpsEvery {
			// The answer fills in this sample (see takeTPSLocked).
			i.tpsAsked = now
			io.WriteString(i.stdin, "tps\n")
		}
	}
	i.history = append(i.history, s)
	cut := 0
	for cut < len(i.history) && now.Sub(i.history[cut].At) > historyKeep {
		cut++
	}
	if cut > 0 {
		i.history = slices.Clone(i.history[cut:])
	}
}

// History returns the usage samples taken since the given time, oldest
// first.
func (i *Instance) History(since time.Time) []Sample {
	i.mu.Lock()
	defer i.mu.Unlock()
	n, _ := slices.BinarySearchFunc(i.history, since, func(s Sample, t time.Time) int { return s.At.Compare(t) })
	return slices.Clone(i.history[n:])
}
