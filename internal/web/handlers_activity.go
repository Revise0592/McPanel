package web

import (
	"net/http"

	"github.com/Revise0592/McPanel/internal/activity"
)

// cardEvents is how many events the console page shows.
const cardEvents = 6

type activityData struct {
	serverView
	Events []activity.Event
	More   bool // there are older events than the ones shown
}

func (a *App) activityFor(id string, n int) activityData {
	evs := a.Mgr.Activity().List(id, 0)
	d := activityData{Events: evs}
	if n > 0 && len(evs) > n {
		d.Events, d.More = evs[:n], true
	}
	return d
}

// activityPage shows a server's full history, or with view=card the short
// list on the console page.
func (a *App) activityPage(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	if r.FormValue("view") == "card" {
		d := a.activityFor(sv.ID, cardEvents)
		d.serverView = a.view(r, sv)
		a.partial(w, "activity-card", d)
		return
	}
	d := a.activityFor(sv.ID, 0)
	d.serverView = a.view(r, sv)
	a.render(w, r, "activity", sv.Name+" activity", "servers", d)
}
