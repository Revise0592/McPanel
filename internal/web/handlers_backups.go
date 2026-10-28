package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/backup"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/store"
)

type backupsData struct {
	serverView
	Backups   []backup.Backup
	Intervals []int
	Busy      bool
	Total     int64 // size of all this server's backups
	Free      int64 // free space on the backup disk; -1 when unknown
	CopyDir   string
}

func (a *App) backupsPage(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	list, err := backup.List(a.Mgr.DataDir(), sv.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d := backupsData{serverView: a.view(r, sv), Backups: list, Intervals: []int{0, 1, 3, 6, 12, 24, 48, 168}}
	for _, b := range list {
		d.Total += b.Size
	}
	d.Free = -1
	if n, ok := backup.FreeSpace(a.Mgr.DataDir()); ok {
		d.Free = n
	}
	d.CopyDir = a.Store.Settings().BackupCopyDir
	d.Tab = "backups"
	d.Busy = d.Status.Task != ""
	a.render(w, r, "backups", sv.Name+" backups", "servers", d)
}

func (a *App) createBackup(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	go func() {
		if _, err := backup.Create(a.Mgr.DataDir(), inst, "manual"); errors.Is(err, mc.ErrBusy) {
			inst.Logf("Backup not started: %v", err)
		}
	}()
	setFlash(w, "Backup started. It will appear here when it's done.")
	back(w, r, "/servers/"+sv.ID+"/backups")
}

func (a *App) saveBackupSchedule(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	_, err := a.Store.UpdateServer(sv.ID, func(s *store.Server) {
		s.BackupEveryHours = formInt(r, "every", s.BackupEveryHours, 0, 24*30)
		s.BackupKeep = formInt(r, "keep", s.BackupKeep, 1, 1000)
	})
	if err != nil {
		setError(w, err.Error())
	} else {
		setFlash(w, "Backup schedule saved.")
	}
	back(w, r, "/servers/"+sv.ID+"/backups")
}

func (a *App) downloadBackup(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	p, err := backup.Path(a.Mgr.DataDir(), sv.ID, r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, _ := f.Stat()
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s"`, sv.ID, r.PathValue("name")))
	http.ServeContent(w, r, r.PathValue("name"), info.ModTime(), f)
}

func (a *App) restoreBackup(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	if _, err := backup.Path(a.Mgr.DataDir(), sv.ID, name); err != nil {
		http.NotFound(w, r)
		return
	}
	worldOnly := r.FormValue("what") == "world"
	go func() {
		if err := backup.Restore(a.Mgr.DataDir(), inst, name, worldOnly); err != nil {
			inst.Record(activity.Failed, "Restore failed: %v", err)
		}
	}()
	if worldOnly {
		setFlash(w, "Restoring the world. The server is backed up first.")
	} else {
		setFlash(w, "Restoring. The current world is backed up first.")
	}
	back(w, r, "/servers/"+sv.ID)
}

func (a *App) deleteBackup(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	if err := backup.Delete(a.Mgr.DataDir(), sv.ID, r.PathValue("name")); err != nil {
		setError(w, "Couldn't delete the backup.")
	} else {
		setFlash(w, "Backup deleted.")
	}
	back(w, r, "/servers/"+sv.ID+"/backups")
}
