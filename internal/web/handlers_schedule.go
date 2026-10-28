package web

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/schedule"
	"github.com/Revise0592/McPanel/internal/store"
)

const maxTasks = 20

type taskRow struct {
	store.Task
	Next time.Time
}

type scheduleData struct {
	serverView
	Rows  []taskRow
	Hours []int
	Clock string // the panel's time and zone, since daily times use it
}

func scheduleURL(id string) string { return "/servers/" + id + "/schedule" }

func (a *App) schedulePage(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	d := scheduleData{serverView: a.view(r, sv), Hours: []int{1, 2, 3, 4, 6, 8, 12}, Clock: time.Now().Format("15:04 MST")}
	for _, t := range sv.Tasks {
		d.Rows = append(d.Rows, taskRow{Task: t, Next: a.Tasks.NextRun(t)})
	}
	d.Tab = "schedule"
	a.render(w, r, "schedule", sv.Name+" schedule", "servers", d)
}

func (a *App) addTask(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	to := scheduleURL(sv.ID)
	t := store.Task{ID: store.RandomHex(6), Action: r.FormValue("action")}
	switch t.Action {
	case store.TaskRestart:
	case store.TaskCommand:
		t.Command = strings.TrimPrefix(strings.TrimSpace(strings.ReplaceAll(r.FormValue("command"), "\n", " ")), "/")
		if t.Command == "" || len(t.Command) > 500 {
			setError(w, "Enter the command to run.")
			back(w, r, to)
			return
		}
	default:
		setError(w, "Choose what to do.")
		back(w, r, to)
		return
	}
	if when := r.FormValue("when"); when == "daily" {
		t.At = r.FormValue("at")
		if _, _, ok := schedule.ParseAt(t.At); !ok {
			setError(w, "Enter a time of day, like 05:00.")
			back(w, r, to)
			return
		}
	} else {
		t.EveryHours, _ = strconv.Atoi(when)
		if t.EveryHours < 1 || t.EveryHours > 24 {
			setError(w, "Choose how often.")
			back(w, r, to)
			return
		}
	}
	full := false
	_, err := a.Store.UpdateServer(sv.ID, func(s *store.Server) {
		if len(s.Tasks) >= maxTasks {
			full = true
			return
		}
		s.Tasks = append(s.Tasks, t)
	})
	switch {
	case err != nil:
		setError(w, err.Error())
	case full:
		setError(w, "A server can have up to 20 scheduled tasks.")
	default:
		a.Tasks.Added(t.ID)
		setFlash(w, "Task added.")
	}
	back(w, r, to)
}

func (a *App) deleteTask(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	id := r.FormValue("task")
	_, err := a.Store.UpdateServer(sv.ID, func(s *store.Server) {
		s.Tasks = slices.DeleteFunc(s.Tasks, func(t store.Task) bool { return t.ID == id })
	})
	if err != nil {
		setError(w, err.Error())
	} else {
		setFlash(w, "Task removed.")
	}
	back(w, r, scheduleURL(sv.ID))
}
