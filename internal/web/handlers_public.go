package web

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"

	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/props"
	"github.com/Revise0592/McPanel/internal/store"
	"github.com/Revise0592/McPanel/internal/velocity"
)

// A server's status page is public: anyone with its link sees whether the
// server is up, who's on and how to join, and nothing else.

// publicView is everything a status page shows. It holds no IDs or paths,
// since the page is public.
type publicView struct {
	Token          string
	Name           string
	MOTD           string
	Type           string
	Version        string
	State          mc.State
	Label          string
	Online         bool
	Players        []string
	MaxPlayers     int
	Address        string
	BedrockAddress string
	MapURL         string
	IconTime       int64
	PanelName      string
}

var formatCodes = regexp.MustCompile(`§.`)

// serverByToken finds the server whose status page token matches.
func (a *App) serverByToken(token string) (store.Server, bool) {
	if len(token) < 16 {
		return store.Server{}, false
	}
	for _, sv := range a.Store.Servers() {
		if sv.StatusToken != "" && subtle.ConstantTimeCompare([]byte(sv.StatusToken), []byte(token)) == 1 {
			return sv, true
		}
	}
	return store.Server{}, false
}

func (a *App) publicLoad(w http.ResponseWriter, r *http.Request) (store.Server, *mc.Instance, bool) {
	sv, ok := a.serverByToken(r.PathValue("token"))
	if !ok {
		http.NotFound(w, r)
		return sv, nil, false
	}
	inst, err := a.Mgr.Get(sv.ID)
	if err != nil {
		http.NotFound(w, r)
		return sv, nil, false
	}
	return sv, inst, true
}

func (a *App) publicView(r *http.Request, sv store.Server, inst *mc.Instance) publicView {
	v := a.view(r, sv)
	p := publicView{
		Token:          sv.StatusToken,
		Name:           sv.Name,
		Type:           sv.Type,
		Version:        sv.Version,
		State:          v.Status.State,
		Label:          v.Status.Label(),
		Online:         v.Status.State == mc.Running,
		Players:        v.Status.Players,
		Address:        v.Address,
		BedrockAddress: v.BedrockAddress,
		MapURL:         v.MapURL,
		IconTime:       v.IconTime,
		PanelName:      a.Store.Settings().PanelName,
		MaxPlayers:     20,
	}
	if v.ViaProxy != "" {
		// Players can only join through the proxy.
		for _, o := range a.Store.Servers() {
			if store.IsProxy(o.Type) && slices.Contains(o.Backends, sv.ID) {
				p.Address = a.view(r, o).Address
			}
		}
	}
	if v.Status.Task != "" {
		// Backups and installs are the panel's business.
		p.Label = "Busy"
		if v.Status.Online() {
			p.Label = "Online"
		}
	}
	if !sv.Installed {
		p.Label = "Being set up"
	}
	if sv.Type == store.TypeVelocity {
		vs := velocity.Read(inst.Dir())
		p.MOTD, p.MaxPlayers = vs.MOTD, vs.MaxPlayers
	} else if f, err := props.Load(filepath.Join(inst.Dir(), "server.properties")); err == nil {
		key := "motd"
		if sv.Type == store.TypeBedrock {
			key = "server-name"
		}
		p.MOTD, _ = f.Get(key)
		p.MaxPlayers, _ = strconv.Atoi(f.GetOr("max-players", "20"))
	}
	p.MOTD = formatCodes.ReplaceAllString(p.MOTD, "")
	if p.MOTD == p.Name {
		p.MOTD = ""
	}
	return p
}

// HeadURL is where the page loads a player's head from.
func (p publicView) HeadURL(name string) string {
	if !store.IsJava(p.Type) {
		return "/heads/-"
	}
	return "/s/" + p.Token + "/heads/" + url.PathEscape(name)
}

func (a *App) statusPage(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.publicLoad(w, r)
	if !ok {
		return
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex")
	a.render(w, r, "status", sv.Name, "", a.publicView(r, sv, inst))
}

func (a *App) statusLive(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.publicLoad(w, r)
	if !ok {
		return
	}
	a.partial(w, "status-live", a.publicView(r, sv, inst))
}

func (a *App) statusIcon(w http.ResponseWriter, r *http.Request) {
	_, inst, ok := a.publicLoad(w, r)
	if !ok {
		return
	}
	setImageCache(w, r)
	http.ServeFile(w, r, iconFile(inst))
}

// statusHead serves heads of players who are on the server right now, so the
// page can't be used to look up anyone else.
func (a *App) statusHead(w http.ResponseWriter, r *http.Request) {
	_, inst, ok := a.publicLoad(w, r)
	if !ok {
		return
	}
	if !slices.Contains(inst.Status().Players, r.PathValue("name")) {
		http.NotFound(w, r)
		return
	}
	a.playerHead(w, r)
}

// statusPageURL is the full public address of a server's status page.
func statusPageURL(r *http.Request, token string) string {
	scheme := "http"
	if viaHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/s/" + token
}

func (a *App) saveStatusPage(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	action := r.FormValue("action")
	msg := ""
	_, err := a.Store.UpdateServer(sv.ID, func(s *store.Server) {
		switch action {
		case "on":
			s.StatusToken = store.RandomHex(16)
			msg = "The status page is on. Share its link with your players."
		case "new":
			s.StatusToken = store.RandomHex(16)
			msg = "The status page has a new link. The old one no longer works."
		case "off":
			s.StatusToken = ""
			msg = "The status page is off."
		}
	})
	if err != nil {
		setError(w, err.Error())
	} else if msg != "" {
		setFlash(w, msg)
	}
	back(w, r, settingsURL(sv.ID, "general")+"#status-page")
}
