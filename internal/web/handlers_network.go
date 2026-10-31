package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Revise0592/McPanel/internal/store"
	"github.com/Revise0592/McPanel/internal/velocity"
)

// A Velocity proxy links other servers into one network: players join the
// proxy and move between its servers with /server. McPanel lists the
// servers in velocity.toml and sets each one up to trust the proxy.

// backendChoice is a server that a proxy can link to.
type backendChoice struct {
	ID      string
	Name    string
	Type    string
	Port    int
	Linked  bool
	Start   bool // players join this one first
	Ready   bool // has made its settings files
	Online  bool
	OtherOf string // linked to a different proxy
}

// networkChoices lists the servers a proxy can link, linked ones first in
// the order players are tried.
func (a *App) networkChoices(proxy store.Server) []backendChoice {
	all := a.Store.Servers()
	other := map[string]string{}
	for _, sv := range all {
		if sv.ID != proxy.ID && store.IsProxy(sv.Type) {
			for _, id := range sv.Backends {
				other[id] = sv.Name
			}
		}
	}
	var out []backendChoice
	for _, sv := range all {
		if !store.IsPaperLike(sv.Type) || !sv.Installed {
			continue
		}
		inst, err := a.Mgr.Get(sv.ID)
		if err != nil {
			continue
		}
		c := backendChoice{
			ID: sv.ID, Name: sv.Name, Type: sv.Type, Port: a.Mgr.Port(sv),
			Linked:  slices.Contains(proxy.Backends, sv.ID),
			Ready:   fileExists(inst.Dir(), velocity.PaperConfig),
			Online:  inst.Status().Online(),
			OtherOf: other[sv.ID],
		}
		c.Start = len(proxy.Backends) > 0 && proxy.Backends[0] == sv.ID
		out = append(out, c)
	}
	slices.SortStableFunc(out, func(x, y backendChoice) int {
		xi, yi := slices.Index(proxy.Backends, x.ID), slices.Index(proxy.Backends, y.ID)
		if xi < 0 {
			xi = len(proxy.Backends)
		}
		if yi < 0 {
			yi = len(proxy.Backends)
		}
		return xi - yi
	})
	return out
}

// proxyOf names the proxy a server is linked to, if any.
func (a *App) proxyOf(id string) string {
	for _, sv := range a.Store.Servers() {
		if store.IsProxy(sv.Type) && slices.Contains(sv.Backends, id) {
			return sv.Name
		}
	}
	return ""
}

func (a *App) saveNetwork(w http.ResponseWriter, r *http.Request) {
	proxy, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	to := settingsURL(proxy.ID, "network")
	if !store.IsProxy(proxy.Type) {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	chosen := r.Form["backend"]
	start := r.FormValue("start")
	secret, err := velocity.Secret(inst.Dir())
	if err != nil {
		setError(w, "Couldn't read the forwarding secret: "+err.Error())
		back(w, r, to)
		return
	}

	var linked []string
	var entries []velocity.Backend
	var problems, touched []string
	for _, c := range a.networkChoices(proxy) {
		want := slices.Contains(chosen, c.ID)
		if want && c.OtherOf != "" {
			problems = append(problems, fmt.Sprintf("%s is already linked to %s.", c.Name, c.OtherOf))
			continue
		}
		b, err := a.Mgr.Get(c.ID)
		if err != nil {
			continue
		}
		switch {
		case want:
			if err := velocity.LinkPaper(b.Dir(), secret); err != nil {
				if errors.Is(err, velocity.ErrNotStarted) {
					problems = append(problems, fmt.Sprintf("%s: start it once first, so it creates its settings files.", c.Name))
				} else {
					problems = append(problems, fmt.Sprintf("%s: %v", c.Name, err))
				}
				continue
			}
			if c.ID == start {
				linked = append([]string{c.ID}, linked...)
				entries = append([]velocity.Backend{{Name: velocity.ServerName(c.Name), Port: c.Port}}, entries...)
			} else {
				linked = append(linked, c.ID)
				entries = append(entries, velocity.Backend{Name: velocity.ServerName(c.Name), Port: c.Port})
			}
			if !c.Linked {
				touched = append(touched, c.Name)
			}
		case c.Linked:
			if err := velocity.UnlinkPaper(b.Dir()); err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", c.Name, err))
				continue
			}
			touched = append(touched, c.Name)
		}
	}
	if err := velocity.Link(inst.Dir(), entries); err != nil {
		setError(w, "Couldn't save velocity.toml: "+err.Error())
		back(w, r, to)
		return
	}
	if _, err := a.Store.UpdateServer(proxy.ID, func(s *store.Server) { s.Backends = linked }); err != nil {
		setError(w, err.Error())
		back(w, r, to)
		return
	}

	msg := "Saved."
	if len(touched) > 0 || inst.Status().Online() {
		names := append([]string{proxy.Name}, touched...)
		msg += " Restart " + joinNames(names) + " for the change to take effect."
	}
	if len(problems) > 0 {
		setError(w, msg+" Not linked: "+strings.Join(problems, " "))
	} else {
		setFlash(w, msg)
	}
	back(w, r, to)
}

func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func (a *App) saveProxySettings(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	to := settingsURL(sv.ID, "game")
	if !store.IsProxy(sv.Type) {
		http.NotFound(w, r)
		return
	}
	port, err := strconv.Atoi(strings.TrimSpace(r.FormValue("port")))
	if err != nil || port < 1 || port > 65535 {
		setError(w, "Ports must be between 1 and 65535.")
		back(w, r, to)
		return
	}
	if who := a.Mgr.UsedPorts(sv.ID)[port]; who != "" {
		setError(w, fmt.Sprintf("Port %d is already used by %s.", port, who))
		back(w, r, to)
		return
	}
	maxPlayers, err := strconv.Atoi(strings.TrimSpace(r.FormValue("maxPlayers")))
	if err != nil || maxPlayers < 0 {
		setError(w, "Max players must be a whole number.")
		back(w, r, to)
		return
	}
	motd := strings.TrimSpace(strings.ReplaceAll(r.FormValue("motd"), "\n", " "))
	if err := velocity.Save(inst.Dir(), velocity.Settings{Port: port, MOTD: motd, MaxPlayers: maxPlayers}); err != nil {
		setError(w, err.Error())
	} else {
		setFlash(w, "Proxy settings saved."+restartHint(inst))
	}
	back(w, r, to)
}

// settingsFile is the file the Advanced section edits.
func settingsFile(sv store.Server) string {
	if store.IsProxy(sv.Type) {
		return velocity.ConfigFile
	}
	return "server.properties"
}

func fileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}
