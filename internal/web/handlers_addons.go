package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/addons"
)

type addonsData struct {
	serverView
	Addons []addons.Addon
	Folder string
}

type searchData struct {
	ID   string
	Hits []addons.Hit
	Err  string
}

func (a *App) addonsPage(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	if !addons.Supported(sv.Type) {
		http.NotFound(w, r)
		return
	}
	list, _ := addons.List(sv.Type, inst.Dir())
	d := addonsData{serverView: a.view(r, sv), Addons: list, Folder: addons.Folder(sv.Type)}
	d.Tab = "addons"
	a.render(w, r, "addons", sv.Name+" "+addons.Noun(sv.Type)+"s", "servers", d)
}

func (a *App) addonsSearch(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	if !addons.Supported(sv.Type) {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	hits, err := addons.Search(ctx, sv.Type, sv.Version, strings.TrimSpace(r.FormValue("q")))
	d := searchData{ID: sv.ID, Hits: hits}
	if err != nil {
		d.Err = "Search failed: " + err.Error()
	}
	a.partial(w, "addon-results", d)
}

func (a *App) addonsInstall(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	to := "/servers/" + sv.ID + "/addons"
	if !addons.Supported(sv.Type) {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	names, err := addons.Install(ctx, sv.Type, sv.Version, r.FormValue("project"), inst.Dir(), inst.Logf)
	switch {
	case err != nil:
		setError(w, "Install failed: "+err.Error())
	case len(names) == 1:
		setFlash(w, fmt.Sprintf("Installed %s.%s", names[0], restartHint(inst)))
	default:
		setFlash(w, fmt.Sprintf("Installed %s (with dependencies).%s", strings.Join(names, ", "), restartHint(inst)))
	}
	back(w, r, to)
}

func (a *App) addonsToggle(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	if err := addons.Toggle(sv.Type, inst.Dir(), r.FormValue("file")); err != nil {
		setError(w, err.Error())
	} else {
		setFlash(w, "Saved."+restartHint(inst))
	}
	back(w, r, "/servers/"+sv.ID+"/addons")
}

func (a *App) addonsRemove(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	if err := addons.Remove(sv.Type, inst.Dir(), r.FormValue("file")); err != nil {
		setError(w, err.Error())
	} else {
		setFlash(w, "Removed."+restartHint(inst))
	}
	back(w, r, "/servers/"+sv.ID+"/addons")
}
