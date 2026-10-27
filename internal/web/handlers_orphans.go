package web

import (
	"fmt"
	"net/http"
	"os"

	"github.com/Revise0592/McPanel/internal/backup"
)

// Backups of deleted servers are kept on purpose. These pages let the user
// look through them, download any worth keeping, and free the space.

const orphansURL = "/backups/deleted"

func (a *App) orphans() []backup.Orphan {
	list, _ := backup.Orphans(a.Mgr.DataDir(), func(id string) bool {
		_, err := a.Store.Server(id)
		return err == nil
	})
	return list
}

// orphan finds the deleted server named in the URL, writing a 404 if its
// folder is missing or the server still exists.
func (a *App) orphan(w http.ResponseWriter, r *http.Request) (backup.Orphan, bool) {
	id := r.PathValue("id")
	for _, o := range a.orphans() {
		if o.ID == id {
			return o, true
		}
	}
	http.NotFound(w, r)
	return backup.Orphan{}, false
}

// orphanSummary is the dashboard notice.
type orphanSummary struct {
	Servers int
	Size    int64
}

func (a *App) orphanSummary() orphanSummary {
	var s orphanSummary
	for _, o := range a.orphans() {
		s.Servers++
		s.Size += o.Size
	}
	return s
}

func (a *App) orphansPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "orphans", "Backups from deleted servers", "servers", a.orphans())
}

func (a *App) downloadOrphan(w http.ResponseWriter, r *http.Request) {
	o, ok := a.orphan(w, r)
	if !ok {
		return
	}
	p, err := backup.Path(a.Mgr.DataDir(), o.ID, r.PathValue("name"))
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
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s"`, o.ID, r.PathValue("name")))
	http.ServeContent(w, r, r.PathValue("name"), info.ModTime(), f)
}

func (a *App) deleteOrphan(w http.ResponseWriter, r *http.Request) {
	o, ok := a.orphan(w, r)
	if !ok {
		return
	}
	err := backup.Delete(a.Mgr.DataDir(), o.ID, r.PathValue("name"))
	// Remove the folder along with its last backup.
	if err == nil && len(o.Backups) == 1 {
		err = backup.DeleteFolder(a.Mgr.DataDir(), o.ID)
	}
	if err != nil {
		setError(w, "Couldn't delete the backup.")
	} else {
		setFlash(w, "Backup deleted.")
	}
	back(w, r, orphansURL)
}

func (a *App) deleteOrphanFolder(w http.ResponseWriter, r *http.Request) {
	o, ok := a.orphan(w, r)
	if !ok {
		return
	}
	if err := backup.DeleteFolder(a.Mgr.DataDir(), o.ID); err != nil {
		setError(w, "Couldn't delete the backups: "+err.Error())
	} else {
		setFlash(w, fmt.Sprintf("Deleted the backups of %s, freeing %s.", o.Name, backup.HumanSize(o.Size)))
	}
	back(w, r, orphansURL)
}
