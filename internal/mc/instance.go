// Package mc runs Minecraft server processes and tracks their state.
package mc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/store"
)

// State of a server process.
type State string

const (
	Stopped  State = "stopped"
	Starting State = "starting"
	Running  State = "running"
	Stopping State = "stopping"
	Crashed  State = "crashed"
)

const (
	logLines    = 1000
	stopTimeout = 60 * time.Second
	crashWindow = 10 * time.Minute
	maxCrashes  = 3
)

var (
	// Paper colours some console output (join messages included), so lines
	// are cleaned before they're parsed or stored.
	ansiCodes   = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\r`)
	javaJoin    = regexp.MustCompile(`: ([A-Za-z0-9_.]{1,16}) joined the game`)
	javaLogin   = regexp.MustCompile(`: ([A-Za-z0-9_.]{1,16})\[/[^\]]*\] logged in with entity id`)
	javaLeave   = regexp.MustCompile(`: ([A-Za-z0-9_.]{1,16}) left the game`)
	javaReject  = regexp.MustCompile(`([A-Za-z0-9_.]{1,16}) \(/[^)]*\) lost connection: You are not white-?listed`)
	javaReject2 = regexp.MustCompile(`name=([A-Za-z0-9_.]{1,16}).*You are not white-?listed`)
	bedJoin     = regexp.MustCompile(`Player connected: (.+?), xuid`)
	bedLeave    = regexp.MustCompile(`Player disconnected: (.+?), xuid`)
)

// ErrNotRunning is returned when a command needs a running server.
var ErrNotRunning = errors.New("the server is not running")

// ErrBusy is returned when another job is already running on the server.
var ErrBusy = errors.New("busy")

// Instance is one managed server.
type Instance struct {
	ID string
	m  *Manager

	mu          sync.Mutex
	state       State
	task        string
	progress    int // percent done of task, -1 when unknown
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	done        chan struct{}
	stopReq     bool
	restarting  bool // stopped by Restart, so the next start is a restart
	crashes     []time.Time
	startedAt   time.Time
	lines       []string
	subs        map[chan string]struct{}
	players     map[string]time.Time
	rejected    []string
	lastPlayers time.Time
	stats       map[string]PlayerStats
	statsDirty  bool
	saveMu      sync.Mutex // serialises writes of the stats file

	// Resource use, see Usage.
	paper    bool
	cpuPrev  cpuSample
	cpuLast  float64
	tps      float64
	tpsAt    time.Time
	tpsAsked time.Time
	histPrev cpuSample
	history  []Sample
}

func newInstance(m *Manager, id string) *Instance {
	i := &Instance{ID: id, m: m, state: Stopped, subs: map[chan string]struct{}{}, players: map[string]time.Time{}, stats: map[string]PlayerStats{}}
	i.loadStats()
	return i
}

// Dir is the server's working directory.
func (i *Instance) Dir() string { return filepath.Join(i.m.dataDir, "servers", i.ID) }

// Manager is the manager that owns the instance.
func (i *Instance) Manager() *Manager { return i.m }

// Def returns the current saved definition.
func (i *Instance) Def() (store.Server, error) { return i.m.store.Server(i.ID) }

// Status is a snapshot for display.
type Status struct {
	State     State
	Task      string
	Progress  int // percent done of Task, -1 when unknown
	Players   []string
	Rejected  []string
	StartedAt time.Time
}

// Online reports whether the process is up.
func (s Status) Online() bool { return s.State == Running || s.State == Starting }

// Label is a short human description of the state.
func (s Status) Label() string {
	if s.Task != "" {
		return s.Task
	}
	switch s.State {
	case Running:
		return "Online"
	case Starting:
		return "Starting"
	case Stopping:
		return "Stopping"
	case Crashed:
		return "Crashed"
	}
	return "Offline"
}

// Status returns a snapshot of the instance.
func (i *Instance) Status() Status {
	i.mu.Lock()
	defer i.mu.Unlock()
	st := Status{State: i.state, Task: i.task, Progress: i.progress, StartedAt: i.startedAt, Rejected: slices.Clone(i.rejected)}
	for p := range i.players {
		st.Players = append(st.Players, p)
	}
	slices.Sort(st.Players)
	return st
}

// LastPlayerActivity is the last time anyone was seen online.
func (i *Instance) LastPlayerActivity() time.Time {
	i.mu.Lock()
	defer i.mu.Unlock()
	if len(i.players) > 0 {
		return time.Now()
	}
	return i.lastPlayers
}

// Logf writes a panel message to the console.
func (i *Instance) Logf(format string, args ...any) {
	i.addLine("[McPanel] " + fmt.Sprintf(format, args...))
}

// Record writes a panel message to the console and keeps it in the server's
// activity history.
func (i *Instance) Record(kind activity.Kind, format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	i.Logf("%s", text)
	i.m.activity.Add(i.ID, activity.Event{Kind: kind, Text: text})
}

func (i *Instance) addLine(line string) {
	line = ansiCodes.ReplaceAllString(line, "")
	i.mu.Lock()
	if i.takeTPSLocked(line) {
		i.mu.Unlock()
		return
	}
	i.lines = append(i.lines, line)
	if len(i.lines) > logLines {
		i.lines = slices.Clone(i.lines[len(i.lines)-logLines:])
	}
	for ch := range i.subs {
		select {
		case ch <- line:
		default:
		}
	}
	ready := i.parseLocked(line)
	restarted := ready && i.restarting
	if ready {
		i.restarting = false
	}
	dirty := i.statsDirty
	i.mu.Unlock()
	if dirty {
		i.saveStats()
	}
	if restarted {
		i.m.activity.Add(i.ID, activity.Event{Kind: activity.Power, Text: "Restarted"})
	} else if ready {
		i.m.activity.Add(i.ID, activity.Event{Kind: activity.Power, Text: "Started"})
	}
}

// parseLocked tracks state and players from a console line. It reports
// whether the line shows the server has finished starting.
func (i *Instance) parseLocked(line string) (ready bool) {
	if i.state == Starting {
		if (strings.Contains(line, "Done (") && strings.Contains(line, "help")) || strings.Contains(line, "Server started.") {
			i.state = Running
			ready = true
		}
	}
	for _, re := range []*regexp.Regexp{javaJoin, javaLogin, bedJoin} {
		if m := re.FindStringSubmatch(line); m != nil {
			i.joinedLocked(m[1], time.Now())
			i.rejected = slices.DeleteFunc(i.rejected, func(s string) bool { return s == m[1] })
			i.lastPlayers = time.Now()
			return ready
		}
	}
	for _, re := range []*regexp.Regexp{javaLeave, bedLeave} {
		if m := re.FindStringSubmatch(line); m != nil {
			i.leftLocked(m[1], time.Now())
			i.lastPlayers = time.Now()
			return ready
		}
	}
	for _, re := range []*regexp.Regexp{javaReject, javaReject2} {
		if m := re.FindStringSubmatch(line); m != nil {
			if !slices.Contains(i.rejected, m[1]) {
				i.rejected = append(i.rejected, m[1])
				if len(i.rejected) > 10 {
					i.rejected = i.rejected[1:]
				}
			}
			return ready
		}
	}
	return ready
}

// ClearRejected forgets a turned-away player.
func (i *Instance) ClearRejected(name string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.rejected = slices.DeleteFunc(i.rejected, func(s string) bool { return s == name })
}

// Subscribe returns recent lines and a channel of new ones. Call the
// returned cancel func when done.
func (i *Instance) Subscribe() ([]string, <-chan string, func()) {
	ch := make(chan string, 256)
	i.mu.Lock()
	backlog := slices.Clone(i.lines)
	i.subs[ch] = struct{}{}
	i.mu.Unlock()
	return backlog, ch, func() {
		i.mu.Lock()
		delete(i.subs, ch)
		i.mu.Unlock()
	}
}

// BeginTask marks a long-running job (install, backup, restore). It fails if
// another job is in progress.
func (i *Instance) BeginTask(name string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.task != "" {
		return fmt.Errorf("%w: %s", ErrBusy, strings.ToLower(i.task))
	}
	i.task = name
	i.progress = -1
	return nil
}

// SetProgress records how far the current job has got. A total of zero or
// less means the size isn't known.
func (i *Instance) SetProgress(done, total int64) {
	pct := -1
	if total > 0 {
		pct = int(min(done*100/total, 100))
	}
	i.mu.Lock()
	if i.task != "" {
		i.progress = pct
	}
	i.mu.Unlock()
}

// EndTask clears the current job.
func (i *Instance) EndTask() {
	i.mu.Lock()
	i.task = ""
	i.progress = -1
	i.mu.Unlock()
}

// Start launches the server in the background.
func (i *Instance) Start() error {
	def, err := i.Def()
	if err != nil {
		return err
	}
	if !def.Installed {
		return errors.New("the server software is not installed yet")
	}
	i.mu.Lock()
	if i.task == "Installing" || i.task == "Restoring" {
		i.mu.Unlock()
		return fmt.Errorf("wait until %s has finished", strings.ToLower(i.task))
	}
	if i.state == Running || i.state == Starting || i.state == Stopping {
		i.mu.Unlock()
		return nil
	}
	i.state = Starting
	i.stopReq = false
	i.mu.Unlock()
	go i.run(def)
	return nil
}

func (i *Instance) fail(format string, args ...any) {
	i.Record(activity.Failed, format, args...)
	i.mu.Lock()
	i.state = Stopped
	i.restarting = false
	i.mu.Unlock()
}

func (i *Instance) run(def store.Server) {
	dir := i.Dir()
	var cmd *exec.Cmd
	if store.IsJava(def.Type) {
		java, err := install.JavaPath(context.Background(), i.m.dataDir, def.JavaMajor, i.Logf)
		if err != nil {
			i.fail("Could not get Java: %v", err)
			return
		}
		cmd = exec.Command(java, javaArgs(def)...)
		if def.BedrockPort > 0 {
			// Geyser reads its settings from GEYSER_* variables, so the
			// port McPanel picked wins over its config file.
			cmd.Env = append(os.Environ(), fmt.Sprintf("GEYSER_BEDROCK_PORT=%d", def.BedrockPort))
		}
	} else {
		cmd = exec.Command(filepath.Join(dir, def.Executable))
		cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH=.")
	}
	cmd.Dir = dir
	// Own process group, so Ctrl+C on the panel doesn't kill servers
	// before they can save.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		i.fail("Could not start: %v", err)
		return
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		i.fail("Could not start: %v", err)
		return
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	i.mu.Lock()
	cancelled := i.stopReq
	i.mu.Unlock()
	if cancelled {
		pr.Close()
		pw.Close()
		i.Logf("Start cancelled")
		i.mu.Lock()
		i.state = Stopped
		i.mu.Unlock()
		return
	}
	i.Logf("Starting %s", def.Name)
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		i.fail("Could not start: %v", err)
		return
	}
	pw.Close()
	done := make(chan struct{})
	i.mu.Lock()
	i.cmd, i.stdin, i.done, i.startedAt = cmd, stdin, done, time.Now()
	i.paper, i.cpuPrev, i.tpsAt, i.tpsAsked = isPaper(def.Type), cpuSample{}, time.Time{}, time.Time{}
	i.mu.Unlock()

	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			i.addLine(sc.Text())
		}
		pr.Close()
	}()

	err = cmd.Wait()
	close(done)
	i.mu.Lock()
	i.cmd, i.stdin = nil, nil
	if len(i.players) > 0 {
		i.lastPlayers = time.Now()
	}
	now := time.Now()
	for p := range i.players {
		i.leftLocked(p, now)
	}
	requested := i.stopReq
	restarting := i.restarting
	restart := false
	if requested {
		i.state = Stopped
	} else {
		i.state = Crashed
		i.restarting = false
		now := time.Now()
		i.crashes = slices.DeleteFunc(append(i.crashes, now), func(t time.Time) bool { return now.Sub(t) > crashWindow })
		cur, derr := i.m.store.Server(i.ID)
		restart = derr == nil && cur.RestartOnCrash && len(i.crashes) < maxCrashes
	}
	i.mu.Unlock()
	i.saveStats()

	if requested {
		i.Logf("Server stopped")
		if !restarting {
			i.m.activity.Add(i.ID, activity.Event{Kind: activity.Power, Text: "Stopped"})
		}
		return
	}
	i.Record(activity.Crash, "Server exited unexpectedly (%v)", err)
	if restart {
		i.Logf("Restarting in 5 seconds")
		time.AfterFunc(5*time.Second, func() {
			if i.Status().State == Crashed {
				if err := i.Start(); err != nil {
					i.Record(activity.Failed, "Restart after crash failed: %v", err)
				}
			}
		})
	} else if len(i.crashes) >= maxCrashes {
		i.Record(activity.Failed, "Crashed %d times in %d minutes, not restarting automatically", maxCrashes, int(crashWindow.Minutes()))
	}
}

// Send writes a console command to the server.
func (i *Instance) Send(command string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.stdin == nil || (i.state != Running && i.state != Starting) {
		return ErrNotRunning
	}
	command = strings.TrimSpace(strings.ReplaceAll(command, "\n", " "))
	if command == "" {
		return nil
	}
	_, err := io.WriteString(i.stdin, command+"\n")
	return err
}

// SendAndWait sends a command and waits for a console line matching match.
func (i *Instance) SendAndWait(ctx context.Context, command string, match func(string) bool) error {
	_, ch, cancel := i.Subscribe()
	defer cancel()
	if err := i.Send(command); err != nil {
		return err
	}
	for {
		select {
		case l := <-ch:
			if match(l) {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Stop asks the server to shut down and returns immediately.
func (i *Instance) Stop() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state == Crashed {
		i.state = Stopped
		return
	}
	if i.cmd == nil || i.state == Stopping {
		if i.state == Starting {
			// Still fetching Java; the run goroutine will notice.
			i.stopReq = true
		}
		return
	}
	i.stopReq = true
	i.state = Stopping
	io.WriteString(i.stdin, "stop\n")
	done, pid := i.done, i.cmd.Process.Pid
	go func() {
		select {
		case <-done:
		case <-time.After(stopTimeout):
			i.Logf("Server did not stop in time, forcing it to close")
			syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()
}

// StopAndWait stops the server and blocks until it has exited.
func (i *Instance) StopAndWait(ctx context.Context) error {
	i.Stop()
	for {
		i.mu.Lock()
		done, st := i.done, i.state
		i.mu.Unlock()
		if st == Stopped || st == Crashed {
			return nil
		}
		if done == nil {
			// Starting but no process yet.
			select {
			case <-time.After(200 * time.Millisecond):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		select {
		case <-done:
			// The run goroutine updates state right after done closes.
			time.Sleep(50 * time.Millisecond)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Restart stops then starts the server.
func (i *Instance) Restart() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout+10*time.Second)
		defer cancel()
		i.mu.Lock()
		i.restarting = i.state == Running || i.state == Starting
		i.mu.Unlock()
		err := i.StopAndWait(ctx)
		if err == nil {
			err = i.Start()
		}
		if err != nil {
			i.mu.Lock()
			i.restarting = false
			i.mu.Unlock()
			i.Record(activity.Failed, "Restart failed: %v", err)
		}
	}()
}

// Kill force-closes the server without saving.
func (i *Instance) Kill() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.cmd == nil {
		return
	}
	i.stopReq = true
	i.state = Stopping
	syscall.Kill(-i.cmd.Process.Pid, syscall.SIGKILL)
	go i.Logf("Force closing the server")
}
