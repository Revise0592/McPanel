// Package updates checks in the background for newer server builds and
// add-on releases.
package updates

import (
	"context"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/addons"
	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/store"
)

// Every is how often all servers are checked.
const Every = 6 * time.Hour

// Info is what the last check found for one server.
type Info struct {
	// Version is the Minecraft version that was checked.
	Version string
	// LatestBuild is the newest build for the installed version (see
	// install.LatestBuild). Compare it with the server's own build, which
	// may have changed since the check.
	LatestBuild string
	// Addons maps add-on file names to newer releases.
	Addons  map[string]addons.Update
	Checked time.Time
	Err     string
}

// Checker holds the latest results. It is safe for concurrent use.
type Checker struct {
	st  *store.Store
	mgr *mc.Manager

	mu   sync.Mutex
	info map[string]Info
}

// New returns a checker with no results yet.
func New(st *store.Store, mgr *mc.Manager) *Checker {
	return &Checker{st: st, mgr: mgr, info: map[string]Info{}}
}

// Get returns the last result for a server.
func (c *Checker) Get(id string) Info {
	c.mu.Lock()
	defer c.mu.Unlock()
	in := c.info[id]
	in.Addons = maps.Clone(in.Addons)
	return in
}

// ForgetAddon drops a pending add-on update once it has been installed.
func (c *Checker) ForgetAddon(id, file string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if in, ok := c.info[id]; ok && in.Addons != nil {
		delete(in.Addons, file)
	}
}

// Check looks for updates to one server now.
func (c *Checker) Check(ctx context.Context, id string) Info {
	sv, err := c.st.Server(id)
	if err != nil || !sv.Installed {
		return Info{}
	}
	in := Info{Version: sv.Version, Checked: time.Now()}
	if b, err := install.LatestBuild(ctx, sv); err != nil {
		in.Err = err.Error()
	} else {
		in.LatestBuild = b
	}
	if addons.Supported(sv.Type) {
		if inst, err := c.mgr.Get(id); err == nil {
			if u, err := addons.CheckUpdates(ctx, sv.Type, sv.Version, inst.Dir()); err != nil {
				in.Err = err.Error()
			} else {
				in.Addons = u
			}
		}
	}
	c.mu.Lock()
	c.info[id] = in
	c.mu.Unlock()
	c.note(sv, in)
	return in
}

// note adds an activity event for updates not reported before.
func (c *Checker) note(sv store.Server, in Info) {
	hist := c.mgr.Activity()
	var keys, parts []string
	if install.IsNewerBuild(sv, in.LatestBuild) {
		if key := "build:" + sv.Version + ":" + in.LatestBuild; !hist.Seen(sv.ID, key) {
			keys = append(keys, key)
			what := "build " + in.LatestBuild
			if sv.Type == store.TypeBedrock {
				what = "version " + in.LatestBuild
			} else if store.IsModded(sv.Type) {
				what = "loader " + in.LatestBuild
			}
			parts = append(parts, fmt.Sprintf("%s %s", store.TypeLabel(sv.Type), what))
		}
	}
	if inst, err := c.mgr.Get(sv.ID); err == nil && len(in.Addons) > 0 {
		list, _ := addons.List(sv.Type, inst.Dir())
		var names []string
		for _, ad := range list {
			u, ok := in.Addons[ad.Base()]
			if !ok {
				continue
			}
			if key := "addon:" + ad.Base() + ":" + u.Version; !hist.Seen(sv.ID, key) {
				keys = append(keys, key)
				names = append(names, fmt.Sprintf("%s %s", ad.Title, u.Version))
			}
		}
		slices.Sort(names)
		parts = append(parts, names...)
	}
	if len(keys) == 0 {
		return
	}
	hist.Add(sv.ID, activity.Event{Kind: activity.Available, Text: "Update available: " + strings.Join(parts, ", "), Keys: keys})
}

// Recheck checks one server again in the background, after something on
// it has changed.
func (c *Checker) Recheck(id string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		c.Check(ctx, id)
	}()
}

// Run checks every server shortly after start-up, then every few hours,
// until ctx is cancelled.
func (c *Checker) Run(ctx context.Context) {
	t := time.NewTimer(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		for _, sv := range c.st.Servers() {
			cctx, cancel := context.WithTimeout(ctx, time.Minute)
			if in := c.Check(cctx, sv.ID); in.Err != "" {
				log.Printf("update check %s: %s", sv.Name, in.Err)
			}
			cancel()
		}
		t.Reset(Every)
	}
}
