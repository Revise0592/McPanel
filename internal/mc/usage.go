package mc

import (
	"io"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/store"
)

// Usage is how much of the machine a running server is using.
type Usage struct {
	// CPU is the share of the whole machine, 0 to 100. -1 until there
	// are two samples to compare.
	CPU float64
	// MemoryMB is the memory the process holds.
	MemoryMB int
	// TPS is Paper's ticks per second over the last minute; 0 when unknown.
	// 20 is full speed.
	TPS float64
}

// clockTicks is Linux's USER_HZ, which is 100 on every common system.
const clockTicks = 100

// tpsEvery is how often Paper is asked for its TPS while someone watches.
const tpsEvery = 15 * time.Second

var tpsLine = regexp.MustCompile(`TPS from last 1m, 5m, 15m: \*?([0-9.]+)`)

type cpuSample struct {
	ticks uint64
	at    time.Time
}

// Usage reads the server's current CPU and memory use from /proc, and
// asks Paper for its TPS now and then. It returns false when the server
// isn't running or /proc can't be read.
func (i *Instance) Usage() (Usage, bool) {
	i.mu.Lock()
	if i.cmd == nil || i.cmd.Process == nil || (i.state != Running && i.state != Starting) {
		i.mu.Unlock()
		return Usage{}, false
	}
	pid := i.cmd.Process.Pid
	state := i.state
	i.mu.Unlock()

	ticks, mem, ok := readProc(pid)
	if !ok {
		return Usage{}, false
	}
	now := time.Now()
	u := Usage{CPU: -1, MemoryMB: mem}

	i.mu.Lock()
	defer i.mu.Unlock()
	prev := i.cpuPrev
	switch {
	case prev.at.IsZero() || ticks < prev.ticks:
		i.cpuPrev = cpuSample{ticks, now}
	case now.Sub(prev.at) < time.Second:
		// Several pages polling at once: keep the last figure.
		u.CPU = i.cpuLast
	default:
		secs := float64(ticks-prev.ticks) / clockTicks
		u.CPU = min(100, 100*secs/now.Sub(prev.at).Seconds()/float64(runtime.NumCPU()))
		i.cpuPrev, i.cpuLast = cpuSample{ticks, now}, u.CPU
	}

	if state == Running && i.paper && time.Since(i.tpsAsked) > tpsEvery {
		if i.stdin != nil {
			i.tpsAsked = now
			io.WriteString(i.stdin, "tps\n")
		}
	}
	if time.Since(i.tpsAt) < 2*tpsEvery+5*time.Second {
		u.TPS = i.tps
	}
	return u, true
}

// takeTPSLocked records the answer to a "tps" query the panel sent, and
// reports whether the line was that answer so it can be kept out of the
// console.
func (i *Instance) takeTPSLocked(line string) bool {
	if i.tpsAsked.IsZero() || time.Since(i.tpsAsked) > 5*time.Second {
		return false
	}
	m := tpsLine.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	if v, err := strconv.ParseFloat(strings.TrimSuffix(m[1], "."), 64); err == nil {
		i.tps, i.tpsAt = min(v, 20), time.Now()
		if n := len(i.history); n > 0 && i.history[n-1].TPS == 0 && time.Since(i.history[n-1].At) < 10*time.Second {
			i.history[n-1].TPS = i.tps
		}
	}
	i.tpsAsked = time.Time{}
	return true
}

// readProc returns a process's total CPU time in clock ticks and its
// resident memory in MB.
func readProc(pid int) (ticks uint64, memMB int, ok bool) {
	p := "/proc/" + strconv.Itoa(pid)
	stat, err := os.ReadFile(p + "/stat")
	if err != nil {
		return 0, 0, false
	}
	// The command name can hold spaces, so count fields after its ")".
	s := string(stat)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	if len(f) < 13 {
		return 0, 0, false
	}
	utime, err1 := strconv.ParseUint(f[11], 10, 64)
	stime, err2 := strconv.ParseUint(f[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	status, err := os.ReadFile(p + "/status")
	if err != nil {
		return 0, 0, false
	}
	for l := range strings.Lines(string(status)) {
		if rest, found := strings.CutPrefix(l, "VmRSS:"); found {
			kb, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(rest), " kB"))
			memMB = kb / 1024
		}
	}
	return utime + stime, memMB, true
}

// isPaper reports whether the server answers the "tps" command.
func isPaper(typ string) bool { return typ == store.TypePaper }
