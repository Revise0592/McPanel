package mc

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/geyser"
	"github.com/Revise0592/McPanel/internal/icon"
	"github.com/Revise0592/McPanel/internal/props"
	"github.com/Revise0592/McPanel/internal/store"
	"github.com/Revise0592/McPanel/internal/velocity"
	"github.com/Revise0592/McPanel/internal/wake"
)

// sleepCheckEvery is how often idle servers are looked at.
const sleepCheckEvery = 30 * time.Second

// SleepChoices are the idle times offered in settings, in minutes.
var SleepChoices = []int{5, 10, 15, 30, 60, 120}

// RunSleeper puts idle servers to sleep until ctx is cancelled.
func (m *Manager) RunSleeper(ctx context.Context) {
	t := time.NewTicker(sleepCheckEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			for _, sv := range m.store.Servers() {
				if sv.SleepAfterMins <= 0 || !sv.Installed {
					continue
				}
				if inst, err := m.Get(sv.ID); err == nil && inst.idleFor(now) >= time.Duration(sv.SleepAfterMins)*time.Minute {
					go inst.Sleep(fmt.Sprintf("Asleep: nobody has played for %s", plural(sv.SleepAfterMins, "minute")))
				}
			}
		}
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// idleFor is how long a running server has had nobody on. It's 0 when
// players are on, or the server isn't fully running or is busy with a job.
func (i *Instance) idleFor(now time.Time) time.Duration {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state != Running || i.task != "" || len(i.players) > 0 || i.sleepReq {
		return 0
	}
	since := i.startedAt
	if i.lastPlayers.After(since) {
		since = i.lastPlayers
	}
	return now.Sub(since)
}

// Sleep stops the server, if it's running, and holds its port until a
// player tries to join, which starts it again. reason is kept in the
// activity history.
func (i *Instance) Sleep(reason string) error {
	def, err := i.Def()
	if err != nil {
		return err
	}
	if !def.Installed {
		return errors.New("the server software is not installed yet")
	}
	i.mu.Lock()
	if i.state == Sleeping || i.sleepReq {
		i.mu.Unlock()
		return nil
	}
	i.sleepReq = true
	running := i.state != Stopped && i.state != Crashed
	i.mu.Unlock()
	defer func() {
		i.mu.Lock()
		i.sleepReq = false
		i.mu.Unlock()
	}()
	if running {
		i.Logf("Nobody is on, so the server is going to sleep. It starts again when someone joins.")
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout+30*time.Second)
		err := i.StopAndWait(ctx)
		cancel()
		if err != nil {
			return err
		}
	}
	w := wake.New(func() wake.Info { return i.m.wakeInfo(i.ID) }, i.wake)
	if err := i.m.holdPorts(def, i.Dir(), w); err != nil {
		w.Close()
		i.Record(activity.Failed, "Couldn't go to sleep: %v. The server is stopped and won't start when players join.", err)
		return err
	}
	i.mu.Lock()
	if i.state != Stopped && i.state != Crashed {
		// Started again while the ports were being opened.
		i.mu.Unlock()
		w.Close()
		return nil
	}
	i.state = Sleeping
	i.waker = w
	i.mu.Unlock()
	i.Record(activity.Power, "%s", reason)
	return nil
}

// wake starts a sleeping server for a player trying to join.
func (i *Instance) wake(name string) {
	i.mu.Lock()
	sleeping := i.state == Sleeping
	i.mu.Unlock()
	if !sleeping {
		return
	}
	who := "A Bedrock player"
	if name != "" {
		who = name
	}
	i.Record(activity.Power, "Waking up: %s is joining", who)
	if err := i.Start(); err != nil {
		i.Record(activity.Failed, "Couldn't wake up: %v", err)
	}
}

// listenHost is the address the server is told to listen on, or "" for
// every address.
func listenHost(f *props.File) string {
	h, _ := f.Get("server-ip")
	return h
}

// holdPorts opens the ports players use to reach the server.
func (m *Manager) holdPorts(def store.Server, dir string, w *wake.Waker) error {
	if def.Type == store.TypeVelocity {
		host, port := velocity.Bind(dir)
		return w.ListenJava(net.JoinHostPort(host, strconv.Itoa(port)))
	}
	f, err := props.Load(filepath.Join(dir, "server.properties"))
	if err != nil {
		return err
	}
	host, port := listenHost(f), m.Port(def)
	if port <= 0 {
		return errors.New("no port is set")
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	if def.Type == store.TypeBedrock {
		return w.ListenBedrock(addr)
	}
	if err := w.ListenJava(addr); err != nil {
		return err
	}
	if def.BedrockPort > 0 && geyser.Supported(def.Type) && geyser.Find(dir).On() {
		if err := w.ListenBedrock(net.JoinHostPort(host, strconv.Itoa(def.BedrockPort))); err != nil {
			return fmt.Errorf("Bedrock port: %w", err)
		}
	}
	return nil
}

// wakeInfo is what the server list shows for a sleeping server.
func (m *Manager) wakeInfo(id string) wake.Info {
	def, _ := m.store.Server(id)
	dir := filepath.Join(m.dataDir, "servers", id)
	in := wake.Info{MOTD: def.Name, MaxPlayers: 20, Version: def.Version}
	if def.Type == store.TypeVelocity {
		vs := velocity.Read(dir)
		in.MaxPlayers = vs.MaxPlayers
		if vs.MOTD != "" {
			in.MOTD = vs.MOTD
		}
	} else if f, err := props.Load(filepath.Join(dir, "server.properties")); err == nil {
		key := "motd"
		if def.Type == store.TypeBedrock {
			key = "server-name"
		}
		if v, _ := f.Get(key); v != "" {
			in.MOTD = v
		}
		if n, err := strconv.Atoi(f.GetOr("max-players", "20")); err == nil {
			in.MaxPlayers = n
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, icon.FileName)); err == nil && len(b) < 64<<10 {
		in.Favicon = "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
	}
	return in
}
