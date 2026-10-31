package web

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/modpack"
	"github.com/Revise0592/McPanel/internal/store"
)

// Servers can be set up from Modrinth modpacks, picked from Modrinth or
// uploaded as a .mrpack file.

// packMemory is offered for modpack servers, which need more than plain ones.
var packMemory = []int{2048, 3072, 4096, 6144, 8192, 12288, 16384}

const defaultPackMemory = 4096

type modpacksData struct {
	Query  string
	Hits   []modpack.Hit
	Err    string
	Memory []int
}

func (a *App) modpacksPage(w http.ResponseWriter, r *http.Request) {
	d := modpacksData{Query: r.FormValue("q"), Memory: packMemory}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	hits, err := modpack.Search(ctx, d.Query)
	d.Hits = hits
	if err != nil {
		d.Err = "Couldn't reach Modrinth: " + err.Error()
	}
	a.render(w, r, "modpacks", "New server from a modpack", "servers", d)
}

func (a *App) modpacksSearch(w http.ResponseWriter, r *http.Request) {
	d := modpacksData{Query: r.FormValue("q")}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	hits, err := modpack.Search(ctx, d.Query)
	d.Hits = hits
	if err != nil {
		d.Err = "Couldn't reach Modrinth: " + err.Error()
	}
	a.partial(w, "modpack-results", d)
}

type modpackData struct {
	Project  modpack.Project
	Versions []modpack.Version
	Err      string
	Memory   []int
	Default  int
}

func (a *App) modpackPage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	p, err := modpack.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		setError(w, "Couldn't open that modpack: "+err.Error())
		back(w, r, "/modpacks")
		return
	}
	d := modpackData{Project: p, Memory: packMemory, Default: defaultPackMemory}
	d.Versions, err = modpack.Versions(ctx, p.ID)
	if err != nil {
		d.Err = "Couldn't load its releases: " + err.Error()
	} else if len(d.Versions) == 0 {
		d.Err = "This modpack has no releases for Fabric, NeoForge or Forge, so McPanel can't run it."
	}
	a.render(w, r, "modpack", p.Title, "servers", d)
}

// packTemp is where a modpack waits while its server is set up.
func (a *App) packTemp() (string, error) {
	dir := filepath.Join(a.Mgr.DataDir(), "tmp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "modpack-*.mrpack")
	if err != nil {
		return "", err
	}
	f.Close()
	return f.Name(), nil
}

func (a *App) createFromModpack(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	to := "/modpacks/" + project
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	p, err := modpack.GetProject(ctx, project)
	var vs []modpack.Version
	if err == nil {
		vs, err = modpack.Versions(ctx, p.ID)
	}
	if err != nil {
		setError(w, "Couldn't reach Modrinth: "+err.Error())
		back(w, r, to)
		return
	}
	var v modpack.Version
	for _, c := range vs {
		if c.ID == r.FormValue("version") {
			v = c
		}
	}
	if v.ID == "" || len(v.GameVersions) == 0 {
		setError(w, "Pick a release of the modpack.")
		back(w, r, to)
		return
	}
	sv, ok := a.addServer(w, r, to, v.Type, v.GameVersions[0], func(s *store.Server) {
		s.Modpack = &store.Modpack{Name: p.Title, Version: v.VersionNumber, ProjectID: p.ID, VersionID: v.ID}
	})
	if !ok {
		return
	}
	go a.Mgr.InstallModpack(sv.ID, func(ctx context.Context, logf install.Logf) (string, error) {
		path, err := a.packTemp()
		if err != nil {
			return "", err
		}
		logf("Downloading %s %s from Modrinth", p.Title, v.VersionNumber)
		if err := modpack.Download(ctx, v, path, logf); err != nil {
			os.Remove(path)
			return "", err
		}
		return path, nil
	})
	setFlash(w, "Setting up "+p.Title+". Downloading the loader and mods can take a few minutes; progress shows below.")
	back(w, r, "/servers/"+sv.ID)
}

const maxPackUpload = 512 << 20

func (a *App) uploadModpack(w http.ResponseWriter, r *http.Request) {
	to := "/modpacks"
	r.Body = http.MaxBytesReader(w, r.Body, maxPackUpload)
	file, _, err := r.FormFile("pack")
	if err != nil {
		setError(w, "Choose a .mrpack file to upload (up to 512 MB).")
		back(w, r, to)
		return
	}
	defer file.Close()
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	path, err := a.packTemp()
	if err == nil {
		var out *os.File
		if out, err = os.Create(path); err == nil {
			_, err = io.Copy(out, file)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
		}
	}
	if err != nil {
		os.Remove(path)
		setError(w, "Upload failed: "+err.Error())
		back(w, r, to)
		return
	}
	p, err := modpack.Read(path)
	if err != nil {
		os.Remove(path)
		setError(w, "Couldn't use that pack: "+err.Error()+".")
		back(w, r, to)
		return
	}
	if r.FormValue("name") == "" {
		name := []rune(p.Name)
		r.Form.Set("name", string(name[:min(len(name), 40)]))
	}
	sv, ok := a.addServer(w, r, to, p.Type, p.Minecraft, func(s *store.Server) {
		s.Modpack = &store.Modpack{Name: p.Name, Version: p.Version}
	})
	if !ok {
		os.Remove(path)
		return
	}
	go a.Mgr.InstallModpack(sv.ID, func(context.Context, install.Logf) (string, error) { return path, nil })
	setFlash(w, "Setting up "+p.Name+". Downloading the loader and mods can take a few minutes; progress shows below.")
	back(w, r, "/servers/"+sv.ID)
}
