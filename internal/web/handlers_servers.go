package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/addons"
	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/store"
	"github.com/Revise0592/McPanel/internal/updates"
)

// serverView bundles everything a page needs about one server.
type serverView struct {
	store.Server
	Status    mc.Status
	Address   string
	Port      int
	AddonNoun string
	Tab       string
	IconTime  int64 // server icon's change time; 0 when there is none
	// NewBuild is a newer build of the installed version, if there is one.
	NewBuild     string
	AddonUpdates map[string]addons.Update
	UpdateCheck  updates.Info
}

func (a *App) view(r *http.Request, sv store.Server) serverView {
	v := serverView{Server: sv, Port: a.Mgr.Port(sv)}
	if inst, err := a.Mgr.Get(sv.ID); err == nil {
		v.Status = inst.Status()
		if fi, err := os.Stat(iconFile(inst)); err == nil {
			v.IconTime = fi.ModTime().Unix()
		}
	}
	if v.Port > 0 {
		v.Address = fmt.Sprintf("%s:%d", hostOnly(r), v.Port)
	}
	if addons.Supported(sv.Type) {
		v.AddonNoun = addons.Noun(sv.Type)
	}
	// A check made before a version change says nothing about the new one.
	if in := a.Updates.Get(sv.ID); in.Version == sv.Version {
		v.UpdateCheck = in
		if install.IsNewerBuild(sv, in.LatestBuild) {
			v.NewBuild = in.LatestBuild
		}
		v.AddonUpdates = in.Addons
	}
	return v
}

// load fetches the server named in the URL, writing a 404 if it is missing.
func (a *App) load(w http.ResponseWriter, r *http.Request) (store.Server, *mc.Instance, bool) {
	id := r.PathValue("id")
	sv, err := a.Store.Server(id)
	if err != nil {
		http.NotFound(w, r)
		return sv, nil, false
	}
	inst, err := a.Mgr.Get(id)
	if err != nil {
		http.NotFound(w, r)
		return sv, nil, false
	}
	return sv, inst, true
}

func (a *App) allViews(r *http.Request) []serverView {
	var out []serverView
	for _, sv := range a.Store.Servers() {
		out = append(out, a.view(r, sv))
	}
	return out
}

func (a *App) dashboard(w http.ResponseWriter, r *http.Request) {
	d := struct {
		Servers []serverView
		Orphans orphanSummary
	}{a.allViews(r), a.orphanSummary()}
	a.render(w, r, "dashboard", "Servers", "servers", d)
}

func (a *App) serverCards(w http.ResponseWriter, r *http.Request) {
	a.partial(w, "server-cards", a.allViews(r))
}

type newServerData struct {
	Types    []string
	Type     string
	Versions []string
	Selected string
	Err      string
}

var serverTypes = []string{store.TypePaper, store.TypeVanilla, store.TypeFabric, store.TypeBedrock}

func (a *App) newServerPage(w http.ResponseWriter, r *http.Request) {
	d := newServerData{Types: serverTypes, Type: store.TypePaper}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	vs, err := install.Versions(ctx, d.Type)
	if err != nil {
		d.Err = "Couldn't load versions: " + err.Error()
	}
	d.Versions = vs
	a.render(w, r, "new_server", "New server", "servers", d)
}

func (a *App) versionOptions(w http.ResponseWriter, r *http.Request) {
	typ := r.FormValue("type")
	if !slices.Contains(serverTypes, typ) {
		http.Error(w, "unknown type", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	vs, err := install.Versions(ctx, typ)
	d := newServerData{Type: typ, Versions: vs}
	if err != nil {
		d.Err = "Couldn't load versions: " + err.Error()
	}
	a.partial(w, "version-options", d)
}

func formInt(r *http.Request, key string, def, lo, hi int) int {
	n, err := strconv.Atoi(strings.TrimSpace(r.FormValue(key)))
	if err != nil {
		return def
	}
	return max(lo, min(hi, n))
}

func (a *App) createServer(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) {
		setError(w, msg)
		back(w, r, "/servers/new")
	}
	name := strings.TrimSpace(r.FormValue("name"))
	typ := r.FormValue("type")
	version := r.FormValue("version")
	switch {
	case name == "" || len(name) > 40:
		fail("Give the server a name (up to 40 characters).")
		return
	case !slices.Contains(serverTypes, typ):
		fail("Pick a server type.")
		return
	case version == "":
		fail("Pick a version.")
		return
	case r.FormValue("eula") != "on":
		fail("You need to accept the Minecraft EULA to run a server.")
		return
	}
	bedrock := typ == store.TypeBedrock
	base := 25565
	if bedrock {
		base = 19132
	}
	port := formInt(r, "port", 0, 0, 65535)
	if port == 0 {
		port = a.Mgr.FreePort(base, bedrock)
	} else if who := a.Mgr.UsedPorts("")[port]; who != "" {
		fail(fmt.Sprintf("Port %d is already used by %s.", port, who))
		return
	}

	kv := map[string]string{"server-port": strconv.Itoa(port)}
	if gm := r.FormValue("gamemode"); slices.Contains([]string{"survival", "creative", "adventure"}, gm) {
		kv["gamemode"] = gm
	}
	if d := r.FormValue("difficulty"); slices.Contains([]string{"peaceful", "easy", "normal", "hard"}, d) {
		kv["difficulty"] = d
	}
	// Always write the whitelist setting: newer Java versions default it
	// to on, which would silently lock everyone out on first start.
	whitelist := strconv.FormatBool(r.FormValue("whitelist") == "on")
	if bedrock {
		kv["server-portv6"] = strconv.Itoa(port + 1)
		kv["server-name"] = name
		kv["allow-list"] = whitelist
	} else {
		kv["motd"] = name
		kv["white-list"] = whitelist
		kv["enforce-whitelist"] = whitelist
	}
	sv, err := a.Store.AddServer(store.Server{
		Name:             name,
		Type:             typ,
		Version:          version,
		MemoryMB:         formInt(r, "memory", 2048, 512, 65536),
		RestartOnCrash:   true,
		BackupEveryHours: 24,
		BackupKeep:       7,
		InitialProps:     kv,
	})
	if err != nil {
		fail(err.Error())
		return
	}
	go a.Mgr.Install(sv.ID, version, nil)
	setFlash(w, "Downloading the server files. You can watch progress below.")
	back(w, r, "/servers/"+sv.ID)
}

func (a *App) consolePage(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	v := a.view(r, sv)
	v.Tab = "console"
	a.render(w, r, "console", sv.Name, "servers", v)
}

func (a *App) statusPartial(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	a.partial(w, "server-header", a.view(r, sv))
}

func (a *App) power(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	var err error
	switch r.PathValue("action") {
	case "start":
		err = inst.Start()
	case "stop":
		inst.Stop()
	case "restart":
		inst.Restart()
	case "kill":
		inst.Kill()
	default:
		http.NotFound(w, r)
		return
	}
	// Give the state a moment to change so the redraw reflects it.
	time.Sleep(150 * time.Millisecond)
	if r.Header.Get("HX-Request") == "" {
		if err != nil {
			setError(w, err.Error())
		}
		back(w, r, "/servers/"+sv.ID)
		return
	}
	v := a.view(r, sv)
	if err != nil {
		w.Header().Set("HX-Trigger", fmt.Sprintf(`{"showError": %q}`, err.Error()))
	}
	if r.FormValue("card") != "" {
		a.partial(w, "server-card", v)
		return
	}
	a.partial(w, "server-header", v)
}

func (a *App) consoleStream(w http.ResponseWriter, r *http.Request) {
	_, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	backlog, ch, cancel := inst.Subscribe()
	defer cancel()
	write := func(line string) {
		fmt.Fprintf(w, "data: %s\n\n", line)
	}
	for _, l := range backlog {
		write(l)
	}
	fl.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case l := <-ch:
			write(l)
			// Drain whatever else is queued before flushing.
			for n := len(ch); n > 0; n-- {
				write(<-ch)
			}
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func (a *App) sendCommand(w http.ResponseWriter, r *http.Request) {
	_, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	cmd := strings.TrimPrefix(strings.TrimSpace(r.FormValue("command")), "/")
	if err := inst.Send(cmd); err != nil {
		if errors.Is(err, mc.ErrNotRunning) {
			http.Error(w, "The server isn't running.", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if cmd != "" {
		inst.Logf("> %s", cmd)
	}
	w.WriteHeader(http.StatusNoContent)
}
