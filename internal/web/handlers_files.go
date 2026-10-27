package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Revise0592/McPanel/internal/files"
	"github.com/Revise0592/McPanel/internal/props"
	"github.com/Revise0592/McPanel/internal/store"
)

const maxFileUpload = 512 << 20

type crumb struct {
	Name string
	Path string
}

type filesData struct {
	serverView
	Path      string
	Parent    string
	Crumbs    []crumb
	Entries   []files.Entry
	Datapacks string // shortcut to the world's datapacks folder, if there is one
}

func filesURL(id, rel string) string {
	u := "/servers/" + id + "/files"
	if rel != "" {
		u += "?path=" + url.QueryEscape(rel)
	}
	return u
}

func (a *App) filesPage(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	rel := files.Clean(r.FormValue("path"))
	list, err := files.List(inst.Dir(), rel)
	if err != nil {
		if rel == "" {
			http.Error(w, "Couldn't read the server folder: "+err.Error(), http.StatusInternalServerError)
			return
		}
		setError(w, "That folder doesn't exist.")
		back(w, r, filesURL(sv.ID, ""))
		return
	}
	d := filesData{serverView: a.view(r, sv), Path: rel, Entries: list}
	d.Tab = "files"
	if rel != "" {
		d.Parent = path.Dir(rel)
		if d.Parent == "." {
			d.Parent = ""
		}
		parts := strings.Split(rel, "/")
		for i, p := range parts {
			d.Crumbs = append(d.Crumbs, crumb{Name: p, Path: strings.Join(parts[:i+1], "/")})
		}
	}
	if store.IsJava(sv.Type) {
		world := "world"
		if f, err := props.Load(filepath.Join(inst.Dir(), "server.properties")); err == nil {
			world = f.GetOr("level-name", world)
		}
		if dp := files.Clean(world + "/datapacks"); files.IsDir(inst.Dir(), dp) {
			d.Datapacks = dp
		}
	}
	a.render(w, r, "files", sv.Name+" files", "servers", d)
}

func (a *App) downloadFile(w http.ResponseWriter, r *http.Request) {
	_, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	f, info, err := files.Open(inst.Dir(), files.Clean(r.FormValue("path")))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(info.Name())))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// uploadFiles streams each file straight into the folder, so large uploads
// don't need to fit in memory or a temp folder first.
func (a *App) uploadFiles(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	// The folder comes from the URL: FormValue would read the whole body.
	rel := files.Clean(r.URL.Query().Get("path"))
	to := filesURL(sv.ID, rel)
	r.Body = http.MaxBytesReader(w, r.Body, maxFileUpload)
	mr, err := r.MultipartReader()
	if err != nil {
		setError(w, "Choose files to upload.")
		back(w, r, to)
		return
	}
	n := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			setError(w, uploadErr(err, n))
			back(w, r, to)
			return
		}
		if part.FormName() != "files" || part.FileName() == "" {
			part.Close()
			continue
		}
		err = files.Save(inst.Dir(), rel, part.FileName(), part)
		part.Close()
		if err != nil {
			setError(w, fmt.Sprintf("Couldn't save %s: %s", part.FileName(), uploadErr(err, n)))
			back(w, r, to)
			return
		}
		n++
	}
	switch n {
	case 0:
		setError(w, "Choose files to upload.")
	case 1:
		setFlash(w, "Uploaded 1 file."+restartHint(inst))
	default:
		setFlash(w, fmt.Sprintf("Uploaded %d files.", n)+restartHint(inst))
	}
	back(w, r, to)
}

func uploadErr(err error, saved int) string {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		msg := "Uploads are limited to 512 MB at a time."
		if saved > 0 {
			msg += fmt.Sprintf(" %d file(s) were saved before the limit.", saved)
		}
		return msg
	}
	return err.Error()
}

func (a *App) makeFolder(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	rel := files.Clean(r.FormValue("path"))
	if err := files.Mkdir(inst.Dir(), rel, r.FormValue("name")); err != nil {
		if errors.Is(err, os.ErrExist) {
			err = errors.New("something with that name is already here")
		}
		setError(w, "Couldn't make the folder: "+err.Error())
	}
	back(w, r, filesURL(sv.ID, rel))
}

func (a *App) deleteFile(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	rel := files.Clean(r.FormValue("path"))
	parent := path.Dir(rel)
	if parent == "." {
		parent = ""
	}
	if err := files.Delete(inst.Dir(), rel); err != nil {
		setError(w, "Couldn't delete "+path.Base(rel)+".")
	} else {
		setFlash(w, path.Base(rel)+" deleted.")
	}
	back(w, r, filesURL(sv.ID, parent))
}
