package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/addons"
	"github.com/Revise0592/McPanel/internal/install"
)

type addonsData struct {
	serverView
	Addons []addons.Addon
	Folder string
}

// Update returns the pending update for an add-on, if any.
func (d addonsData) Update(ad addons.Addon) string {
	return d.AddonUpdates[ad.Base()].Version
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

// addonsInstall installs an add-on and its dependencies in the background.
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
	noun := addons.Noun(sv.Type)
	if err := inst.BeginTask("Installing " + noun); err != nil {
		setError(w, "Wait until the server is no longer "+strings.TrimPrefix(err.Error(), "busy: ")+".")
		back(w, r, to)
		return
	}
	project := r.FormValue("project")
	go func() {
		defer inst.EndTask()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		names, err := addons.Install(install.WithProgress(ctx, inst.SetProgress), sv.Type, sv.Version, project, inst.Dir(), inst.Logf)
		a.Updates.Recheck(sv.ID)
		switch {
		case err != nil:
			inst.Record(activity.Failed, "Couldn't install the %s: %v", noun, err)
		case len(names) == 1:
			inst.Record(activity.Info, "Installed %s", names[0])
		default:
			inst.Record(activity.Info, "Installed %s (with dependencies)", strings.Join(names, ", "))
		}
	}()
	setFlash(w, "Installing. The list updates when it's done."+restartHint(inst))
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

// addonsUpdate installs pending updates found by the update check, in the
// background: one add-on, or all of them when no file is given.
func (a *App) addonsUpdate(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	to := "/servers/" + sv.ID + "/addons"
	if !addons.Supported(sv.Type) {
		http.NotFound(w, r)
		return
	}
	list, _ := addons.List(sv.Type, inst.Dir())
	pending := a.Updates.Get(sv.ID).Addons
	only := r.FormValue("file")
	var todo []addons.Addon
	for _, ad := range list {
		if _, ok := pending[ad.Base()]; ok && (only == "" || ad.File == only) {
			todo = append(todo, ad)
		}
	}
	if len(todo) == 0 {
		setError(w, "No updates to install. Try checking again.")
		back(w, r, to)
		return
	}
	noun := addons.Noun(sv.Type)
	if err := inst.BeginTask("Updating " + noun + "s"); err != nil {
		setError(w, "Wait until the server is no longer "+strings.TrimPrefix(err.Error(), "busy: ")+".")
		back(w, r, to)
		return
	}
	go func() {
		defer inst.EndTask()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var done, failed []string
		for n, ad := range todo {
			// Progress runs across all the downloads.
			pctx := install.WithProgress(ctx, func(d, t int64) {
				inst.SetProgress(int64(n)*t+d, int64(len(todo))*t)
			})
			title, err := addons.Apply(pctx, sv.Type, inst.Dir(), ad.File, pending[ad.Base()], inst.Logf)
			if err != nil {
				inst.Logf("Could not update %s: %v", ad.Title, err)
				failed = append(failed, ad.Title)
				continue
			}
			a.Updates.ForgetAddon(sv.ID, ad.Base())
			done = append(done, title)
		}
		if len(done) > 0 {
			inst.Record(activity.Info, "Updated %s", strings.Join(done, ", "))
		}
		if len(failed) > 0 {
			inst.Record(activity.Failed, "Couldn't update %s. Details are in the console.", strings.Join(failed, ", "))
		}
	}()
	if len(todo) == 1 {
		setFlash(w, "Updating "+todo[0].Title+"."+restartHint(inst))
	} else {
		setFlash(w, fmt.Sprintf("Updating %d %ss.%s", len(todo), noun, restartHint(inst)))
	}
	back(w, r, to)
}
