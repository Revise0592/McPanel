package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/geyser"
	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/store"
)

// bedrockHost is the host Bedrock players connect to: the join address's
// host if one is set, otherwise the panel's.
func bedrockHost(r *http.Request, sv store.Server) string {
	if sv.JoinAddress != "" {
		if h, _, err := net.SplitHostPort(sv.JoinAddress); err == nil {
			return h
		}
		return sv.JoinAddress
	}
	return hostOnly(r)
}

// bedrockAction turns Geyser on or off, updates it, or changes its port.
func (a *App) bedrockAction(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	to := "/servers/" + sv.ID + "/addons"
	if !geyser.Supported(sv.Type) {
		http.NotFound(w, r)
		return
	}
	switch r.FormValue("action") {
	case "install":
		if err := inst.BeginTask("Installing Geyser"); err != nil {
			setError(w, "Wait until the server is no longer "+strings.TrimPrefix(err.Error(), "busy: ")+".")
			back(w, r, to)
			return
		}
		if sv.BedrockPort == 0 {
			port := a.Mgr.FreePort(19132, false)
			if _, err := a.Store.UpdateServer(sv.ID, func(s *store.Server) { s.BedrockPort = port }); err != nil {
				inst.EndTask()
				setError(w, err.Error())
				back(w, r, to)
				return
			}
		}
		go func() {
			defer inst.EndTask()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			names, err := geyser.Install(install.WithProgress(ctx, inst.SetProgress), inst.Dir(), inst.Logf)
			if err != nil {
				inst.Record(activity.Failed, "Couldn't set up Bedrock players: %v", err)
				return
			}
			inst.Record(activity.Info, "Installed %s", strings.Join(names, " and "))
		}()
		setFlash(w, "Installing Geyser and Floodgate. The page updates when it's done."+restartHint(inst))
	case "remove":
		if err := geyser.Remove(inst.Dir()); err != nil {
			setError(w, "Couldn't remove Geyser: "+err.Error())
		} else {
			inst.Record(activity.Info, "Removed Geyser and Floodgate")
			setFlash(w, "Bedrock players turned off."+restartHint(inst))
		}
	case "port":
		port := formInt(r, "port", 0, 0, 65535)
		if port < 1 {
			setError(w, "Ports must be between 1 and 65535.")
			back(w, r, to)
			return
		}
		if who := a.Mgr.UsedPorts(sv.ID)[port]; who != "" {
			setError(w, fmt.Sprintf("Port %d is already used by %s.", port, who))
			back(w, r, to)
			return
		}
		if port == a.Mgr.Port(sv) {
			setError(w, "Bedrock players need a different port from Java players.")
			back(w, r, to)
			return
		}
		if _, err := a.Store.UpdateServer(sv.ID, func(s *store.Server) { s.BedrockPort = port }); err != nil {
			setError(w, err.Error())
		} else {
			setFlash(w, "Port saved."+restartHint(inst))
		}
	default:
		http.NotFound(w, r)
		return
	}
	back(w, r, to)
}
