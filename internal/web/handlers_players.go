package web

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/players"
	"github.com/Revise0592/McPanel/internal/store"
)

type playersData struct {
	serverView
	WhitelistOn bool
	Whitelist   []string
	Ops         []string
	Bans        []string
	Running     bool
	Bedrock     bool
	Recent      []players.Recent
}

func (a *App) playersPage(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	d := playersData{serverView: a.view(r, sv), Bedrock: sv.Type == store.TypeBedrock}
	d.Tab = "players"
	d.Running = d.Status.State == mc.Running
	d.WhitelistOn = players.WhitelistEnabled(sv, inst.Dir())
	d.Whitelist, _ = players.Read(sv, inst.Dir(), players.Whitelist)
	d.Ops, _ = players.Read(sv, inst.Dir(), players.Ops)
	d.Bans, _ = players.Read(sv, inst.Dir(), players.Bans)
	d.Recent = players.RecentPlayers(sv, inst.Dir(), inst.PlayerStats(), d.Status.Players, 10)
	a.render(w, r, "players", sv.Name+" players", "servers", d)
}

// headURL is the face icon shown beside a player's name. Bedrock names
// aren't Java accounts, so they always get the placeholder.
func headURL(typ, name string) string {
	if !store.IsJava(typ) {
		return "/heads/-"
	}
	return "/heads/" + url.PathEscape(name)
}

func (a *App) playerHead(w http.ResponseWriter, r *http.Request) {
	b := a.heads.Get(r.Context(), r.PathValue("name"))
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=21600")
	w.Write(b)
}

func (a *App) onlinePartial(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	name := "online-summary"
	if r.FormValue("view") == "manage" {
		name = "online-manage"
	}
	a.partial(w, name, a.view(r, sv))
}

func (a *App) playersAction(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	var err error
	msg := ""
	switch r.FormValue("action") {
	case "kick":
		err = players.Kick(inst, name)
		msg = name + " was kicked."
	case "whitelist-on":
		err = players.SetWhitelistEnabled(sv, inst, true)
		msg = "Only players on the list can join now."
	case "whitelist-off":
		err = players.SetWhitelistEnabled(sv, inst, false)
		msg = "Anyone can join now."
	case "allow":
		err = players.Change(sv, inst, players.Whitelist, name, true)
		inst.ClearRejected(name)
		msg = name + " can join now."
	case "unallow":
		err = players.Change(sv, inst, players.Whitelist, name, false)
		msg = name + " was removed from the list."
	case "dismiss":
		inst.ClearRejected(name)
	case "op":
		err = players.Change(sv, inst, players.Ops, name, true)
		msg = name + " is now an operator."
	case "deop":
		err = players.Change(sv, inst, players.Ops, name, false)
		msg = name + " is no longer an operator."
	case "ban":
		err = players.Change(sv, inst, players.Bans, name, true)
		msg = name + " was banned."
	case "unban":
		err = players.Change(sv, inst, players.Bans, name, false)
		msg = name + " was unbanned."
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	if err != nil {
		setError(w, err.Error())
	} else if msg != "" {
		setFlash(w, msg)
		if inst.Status().State == mc.Running {
			// The server rewrites its list files shortly after the command.
			time.Sleep(500 * time.Millisecond)
		}
	}
	back(w, r, "/servers/"+sv.ID+"/players")
}
