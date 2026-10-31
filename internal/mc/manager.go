package mc

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/props"
	"github.com/Revise0592/McPanel/internal/store"
	"github.com/Revise0592/McPanel/internal/velocity"
)

// Manager owns every server instance.
type Manager struct {
	dataDir  string
	store    *store.Store
	activity *activity.Log

	// OnJoin, if set, is called with each player's name as they join any
	// server. Set it before servers start. It runs while the server's state
	// is locked, so it must be quick and must not call back into the manager.
	OnJoin func(name string)

	mu    sync.Mutex
	insts map[string]*Instance
}

// NewManager creates a manager for servers under dataDir/servers.
func NewManager(dataDir string, st *store.Store) *Manager {
	return &Manager{dataDir: dataDir, store: st, activity: activity.New(dataDir), insts: map[string]*Instance{}}
}

// Activity is the history of what happened to each server.
func (m *Manager) Activity() *activity.Log { return m.activity }

// DataDir returns the panel data directory.
func (m *Manager) DataDir() string { return m.dataDir }

// Settings returns the panel-wide settings.
func (m *Manager) Settings() store.Settings { return m.store.Settings() }

// Get returns the instance for id, creating its runtime state if needed.
func (m *Manager) Get(id string) (*Instance, error) {
	if _, err := m.store.Server(id); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	inst, ok := m.insts[id]
	if !ok {
		inst = newInstance(m, id)
		m.insts[id] = inst
	}
	return inst, nil
}

// StartAutostart starts every installed server marked to start with the
// panel. Servers that sleep when empty start asleep, since nobody can be on
// them yet.
func (m *Manager) StartAutostart() {
	for _, sv := range m.store.Servers() {
		if sv.AutoStart && sv.Installed {
			if inst, err := m.Get(sv.ID); err == nil {
				if sv.SleepAfterMins > 0 {
					inst.Sleep("Asleep until someone joins")
				} else {
					inst.Start()
				}
			}
		}
	}
}

// StopAll stops every running server and waits for them.
func (m *Manager) StopAll(ctx context.Context) {
	m.mu.Lock()
	var all []*Instance
	for _, i := range m.insts {
		all = append(all, i)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, i := range all {
		if !i.Status().Online() {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			i.StopAndWait(ctx)
		}()
	}
	wg.Wait()
}

// Install downloads server software for id. It blocks; run it in a goroutine.
// before, if set, runs after the server has stopped and before files change.
func (m *Manager) Install(id, version string, before func(*Instance) error) error {
	return m.install(id, before, func(ctx context.Context, def store.Server, inst *Instance) (install.Result, error) {
		return install.Install(ctx, m.dataDir, def.Type, version, inst.Dir(), inst.Logf)
	})
}

// InstallBedrockZip installs a Bedrock server from an uploaded zip, which is
// removed afterwards.
func (m *Manager) InstallBedrockZip(id, zipPath, version string, before func(*Instance) error) error {
	defer os.Remove(zipPath)
	return m.install(id, before, func(ctx context.Context, def store.Server, inst *Instance) (install.Result, error) {
		if def.Type != store.TypeBedrock {
			return install.Result{}, fmt.Errorf("only Bedrock servers can be installed from a zip")
		}
		return install.InstallBedrockZip(zipPath, version, inst.Dir(), inst.Logf)
	})
}

// AdoptBedrock marks an imported Bedrock server whose files already include
// the Linux server program as installed, without downloading anything.
func (m *Manager) AdoptBedrock(id, version string) error {
	inst, err := m.Get(id)
	if err != nil {
		return err
	}
	def, err := inst.Def()
	if err != nil {
		return err
	}
	exe := filepath.Join(inst.Dir(), "bedrock_server")
	if _, err := os.Stat(exe); err != nil {
		return err
	}
	os.Chmod(exe, 0o755)
	if err := m.firstRunProperties(def, inst.Dir()); err != nil {
		inst.Logf("Could not update server.properties: %v", err)
	}
	_, err = m.store.UpdateServer(id, func(s *store.Server) {
		s.Version, s.Executable, s.Installed, s.InitialProps = version, "bedrock_server", true, nil
	})
	if err == nil {
		inst.Logf("Imported. Ready to start.")
	}
	return err
}

type installFunc func(ctx context.Context, def store.Server, inst *Instance) (install.Result, error)

func (m *Manager) install(id string, before func(*Instance) error, run installFunc) error {
	inst, err := m.Get(id)
	if err != nil {
		return err
	}
	def, err := inst.Def()
	if err != nil {
		return err
	}
	wasOnline := inst.Status().Online()
	if wasOnline {
		inst.Logf("Stopping the server to change its version")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		err := inst.StopAndWait(ctx)
		cancel()
		if err != nil {
			return err
		}
	}
	if before != nil {
		if err := before(inst); err != nil {
			inst.Record(activity.Failed, "Not changing version: %v", err)
			return err
		}
	}
	if err := inst.BeginTask("Installing"); err != nil {
		return err
	}
	defer inst.EndTask()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	res, err := run(install.WithProgress(ctx, inst.SetProgress), def, inst)
	if err != nil {
		inst.Record(activity.Failed, "Install failed: %v", err)
		return err
	}
	if !def.Installed {
		if err := m.firstRunProperties(def, inst.Dir()); err != nil {
			inst.Logf("Could not write server.properties: %v", err)
		}
	}
	_, err = m.store.UpdateServer(id, func(s *store.Server) {
		s.Version, s.Build, s.Executable, s.JavaMajor = res.Version, res.Build, res.Executable, res.JavaMajor
		s.Installed = true
		s.InitialProps = nil
	})
	if err != nil {
		return err
	}
	label := store.TypeLabel(def.Type) + " " + res.Version
	if res.Build != "" {
		label += " (build " + res.Build + ")"
	}
	inst.Logf("Installed %s. Ready to start.", label)
	m.activity.Add(id, activity.Event{Text: "Installed " + label})
	inst.EndTask()
	if wasOnline {
		return inst.Start()
	}
	return nil
}

func (m *Manager) firstRunProperties(def store.Server, dir string) error {
	if def.Type == store.TypeVelocity {
		port := velocity.DefaultPort
		fmt.Sscan(def.InitialProps["server-port"], &port)
		return velocity.Prepare(dir, filepath.Join(dir, install.VelocityJar), port, cmp.Or(def.InitialProps["motd"], def.Name))
	}
	if len(def.InitialProps) == 0 {
		return nil
	}
	f, err := props.Load(filepath.Join(dir, "server.properties"))
	if err != nil {
		return err
	}
	for k, v := range def.InitialProps {
		f.Set(k, v)
	}
	return f.Save()
}

// UsedPorts returns the ports configured on every server except skipID.
func (m *Manager) UsedPorts(skipID string) map[int]string {
	used := map[int]string{}
	for _, sv := range m.store.Servers() {
		if sv.ID == skipID {
			continue
		}
		p := m.Port(sv)
		if !sv.Installed {
			fmt.Sscan(sv.InitialProps["server-port"], &p)
		}
		if p > 0 {
			used[p] = sv.Name
			if sv.Type == store.TypeBedrock {
				used[p+1] = sv.Name
			}
		}
		if sv.BedrockPort > 0 {
			used[sv.BedrockPort] = sv.Name + " (for Bedrock players)"
		}
	}
	return used
}

// Port returns the configured game port for a server.
func (m *Manager) Port(sv store.Server) int {
	if sv.Type == store.TypeVelocity {
		return velocity.Port(filepath.Join(m.dataDir, "servers", sv.ID))
	}
	f, err := props.Load(filepath.Join(m.dataDir, "servers", sv.ID, "server.properties"))
	if err != nil {
		return 0
	}
	def := "25565"
	if sv.Type == store.TypeBedrock {
		def = "19132"
	}
	var p int
	fmt.Sscan(f.GetOr("server-port", def), &p)
	return p
}

// FreePort returns the first unused port at or above base.
func (m *Manager) FreePort(base int, bedrock bool) int {
	used := m.UsedPorts("")
	for p := base; p < base+1000; p++ {
		if used[p] == "" && (!bedrock || used[p+1] == "") {
			return p
		}
		if bedrock {
			p++
		}
	}
	return base
}

// Remove deletes a stopped server's runtime state and files.
func (m *Manager) Remove(id string) error {
	inst, err := m.Get(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := inst.StopAndWait(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.insts, id)
	m.mu.Unlock()
	if err := m.store.DeleteServer(id); err != nil {
		return err
	}
	m.activity.Remove(id)
	os.Remove(statsPath(m.dataDir, id))
	return os.RemoveAll(inst.Dir())
}
