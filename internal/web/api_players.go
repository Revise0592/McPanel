package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/players"
	"github.com/Revise0592/McPanel/internal/store"
)

type apiRecentPlayer struct {
	Name string    `json:"name"`
	Seen time.Time `json:"seen"`
	// PlayedMins is the time the panel has watched them play.
	PlayedMins int `json:"playedMins"`
}

type apiPlayers struct {
	// Online is who is playing now.
	Online []string `json:"online"`
	// Rejected are players turned away by the whitelist since it started.
	Rejected    []string          `json:"rejected"`
	WhitelistOn bool              `json:"whitelistOn"`
	Whitelist   []string          `json:"whitelist"`
	Ops         []string          `json:"ops"`
	Bans        []string          `json:"bans"`
	Recent      []apiRecentPlayer `json:"recent"`
	// Bedrock servers keep only an allowlist; ops and bans need the
	// server running.
	Bedrock bool `json:"bedrock"`
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (a *App) playersJSON(sv store.Server, inst *mc.Instance) apiPlayers {
	st := inst.Status()
	out := apiPlayers{
		Online:      orEmpty(st.Players),
		Rejected:    orEmpty(st.Rejected),
		WhitelistOn: players.WhitelistEnabled(sv, inst.Dir()),
		Recent:      []apiRecentPlayer{},
		Bedrock:     sv.Type == store.TypeBedrock,
	}
	wl, _ := players.Read(sv, inst.Dir(), players.Whitelist)
	ops, _ := players.Read(sv, inst.Dir(), players.Ops)
	bans, _ := players.Read(sv, inst.Dir(), players.Bans)
	out.Whitelist, out.Ops, out.Bans = orEmpty(wl), orEmpty(ops), orEmpty(bans)
	for _, p := range players.RecentPlayers(sv, inst.Dir(), inst.PlayerStats(), st.Players, 10) {
		out.Recent = append(out.Recent, apiRecentPlayer{Name: p.Name, Seen: p.Seen, PlayedMins: int(p.Played / time.Minute)})
	}
	return out
}

// apiLoadPlayers is apiLoad for servers with player lists of their own.
func (a *App) apiLoadPlayers(w http.ResponseWriter, r *http.Request) (store.Server, *mc.Instance, bool) {
	sv, inst, ok := a.apiLoad(w, r)
	if ok && store.IsProxy(sv.Type) {
		apiFail(w, http.StatusConflict, "A proxy has no player lists of its own. Use the servers behind it.")
		return sv, nil, false
	}
	return sv, inst, ok
}

func (a *App) apiPlayers(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.apiLoadPlayers(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, a.playersJSON(sv, inst))
}

// apiPlayersAction does what the Players page's buttons do. The body is
// {"action": "...", "name": "..."}; it answers with the lists afterwards.
func (a *App) apiPlayersAction(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.apiLoadPlayers(w, r)
	if !ok {
		return
	}
	var req struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	var err error
	switch req.Action {
	case "kick":
		err = players.Kick(inst, name)
	case "whitelist-on":
		err = players.SetWhitelistEnabled(sv, inst, true)
	case "whitelist-off":
		err = players.SetWhitelistEnabled(sv, inst, false)
	case "allow":
		err = players.Change(sv, inst, players.Whitelist, name, true)
		inst.ClearRejected(name)
	case "unallow":
		err = players.Change(sv, inst, players.Whitelist, name, false)
	case "dismiss":
		inst.ClearRejected(name)
	case "op":
		err = players.Change(sv, inst, players.Ops, name, true)
	case "deop":
		err = players.Change(sv, inst, players.Ops, name, false)
	case "ban":
		err = players.Change(sv, inst, players.Bans, name, true)
	case "unban":
		err = players.Change(sv, inst, players.Bans, name, false)
	default:
		apiFail(w, http.StatusBadRequest, "Unknown action.")
		return
	}
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, mc.ErrNotRunning) {
			code = http.StatusConflict
			err = errors.New("the server isn't running")
		}
		apiFail(w, code, sentence(err.Error()))
		return
	}
	if inst.Status().State == mc.Running && req.Action != "dismiss" {
		// The server rewrites its list files shortly after the command.
		time.Sleep(500 * time.Millisecond)
	}
	writeJSON(w, http.StatusOK, a.playersJSON(sv, inst))
}

// sentence turns an error such as "that isn't valid" into "That isn't valid."
func sentence(s string) string {
	s = strings.TrimSuffix(strings.TrimSpace(s), ".")
	if s == "" {
		return "Something went wrong."
	}
	return strings.ToUpper(s[:1]) + s[1:] + "."
}
