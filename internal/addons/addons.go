// Package addons searches Modrinth and manages plugins (Paper) and mods
// (Fabric) on disk.
package addons

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/store"
)

const api = "https://api.modrinth.com/v2"

// Supported reports whether a server type takes add-ons.
func Supported(typ string) bool { return typ == store.TypePaper || typ == store.TypeFabric }

// Noun is "plugin" or "mod".
func Noun(typ string) string {
	if typ == store.TypeFabric {
		return "mod"
	}
	return "plugin"
}

func folder(typ string) string {
	if typ == store.TypeFabric {
		return "mods"
	}
	return "plugins"
}

func loaders(typ string) []string {
	if typ == store.TypeFabric {
		return []string{"fabric"}
	}
	return []string{"paper", "spigot", "bukkit"}
}

// Hit is a search result.
type Hit struct {
	ProjectID   string `json:"project_id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Author      string `json:"author"`
	IconURL     string `json:"icon_url"`
	Downloads   int    `json:"downloads"`
}

func facets(groups ...[]string) string {
	b, _ := json.Marshal(groups)
	return string(b)
}

// Search finds add-ons compatible with the server's type and version.
func Search(ctx context.Context, typ, mcVersion, query string) ([]Hit, error) {
	var cats []string
	for _, l := range loaders(typ) {
		cats = append(cats, "categories:"+l)
	}
	groups := [][]string{cats, {"versions:" + mcVersion}, {"server_side:required", "server_side:optional"}}
	if typ == store.TypeFabric {
		groups = append(groups, []string{"project_type:mod"})
	}
	q := url.Values{}
	q.Set("query", query)
	q.Set("facets", facets(groups...))
	q.Set("limit", "20")
	if query == "" {
		q.Set("index", "downloads")
	}
	var res struct {
		Hits []Hit `json:"hits"`
	}
	err := install.GetJSON(ctx, api+"/search?"+q.Encode(), &res)
	return res.Hits, err
}

type version struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id"`
	VersionNumber string `json:"version_number"`
	Files         []struct {
		URL      string `json:"url"`
		Filename string `json:"filename"`
		Primary  bool   `json:"primary"`
		Hashes   struct {
			SHA512 string `json:"sha512"`
		} `json:"hashes"`
	} `json:"files"`
	Dependencies []struct {
		ProjectID      string `json:"project_id"`
		DependencyType string `json:"dependency_type"`
	} `json:"dependencies"`
}

type project struct {
	Title string `json:"title"`
}

// Addon is an installed jar.
type Addon struct {
	File      string
	Title     string
	Version   string
	ProjectID string
	Enabled   bool
	Size      int64
}

type meta struct {
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Version   string `json:"version"`
}

var metaMu sync.Mutex

func metaPath(serverDir string) string { return filepath.Join(serverDir, ".mcpanel-addons.json") }

func loadMeta(serverDir string) map[string]meta {
	m := map[string]meta{}
	if b, err := os.ReadFile(metaPath(serverDir)); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

func saveMeta(serverDir string, m map[string]meta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath(serverDir), b, 0o644)
}

// Install downloads the newest compatible release of a project, plus any
// required dependencies that aren't already installed.
func Install(ctx context.Context, typ, mcVersion, projectID, serverDir string, logf install.Logf) ([]string, error) {
	metaMu.Lock()
	defer metaMu.Unlock()
	m := loadMeta(serverDir)
	have := map[string]bool{}
	for _, v := range m {
		have[v.ProjectID] = true
	}
	var installed []string
	var walk func(id string, top bool) error
	walk = func(id string, top bool) error {
		if !top && have[id] {
			return nil
		}
		have[id] = true
		title, v, err := installOne(ctx, typ, mcVersion, id, serverDir, m, logf)
		if err != nil {
			return err
		}
		installed = append(installed, title)
		saveMeta(serverDir, m)
		for _, d := range v.Dependencies {
			if d.DependencyType != "required" || d.ProjectID == "" {
				continue
			}
			if err := walk(d.ProjectID, false); err != nil {
				logf("Could not install a required dependency of %s: %v", title, err)
			}
		}
		return nil
	}
	err := walk(projectID, true)
	return installed, err
}

func latestVersion(ctx context.Context, typ, mcVersion, projectID string) (version, error) {
	l, _ := json.Marshal(loaders(typ))
	g, _ := json.Marshal([]string{mcVersion})
	q := url.Values{}
	q.Set("loaders", string(l))
	q.Set("game_versions", string(g))
	var vs []version
	if err := install.GetJSON(ctx, fmt.Sprintf("%s/project/%s/version?%s", api, url.PathEscape(projectID), q.Encode()), &vs); err != nil {
		return version{}, err
	}
	if len(vs) == 0 || len(vs[0].Files) == 0 {
		return version{}, fmt.Errorf("no release for Minecraft %s", mcVersion)
	}
	return vs[0], nil
}

func installOne(ctx context.Context, typ, mcVersion, projectID, serverDir string, m map[string]meta, logf install.Logf) (string, version, error) {
	v, err := latestVersion(ctx, typ, mcVersion, projectID)
	if err != nil {
		return "", v, err
	}
	title, err := installVersion(ctx, typ, v, "", serverDir, m, logf)
	return title, v, err
}

// installVersion downloads v and removes older files from the same project,
// plus replaces (another file being updated that McPanel didn't install).
func installVersion(ctx context.Context, typ string, v version, replaces, serverDir string, m map[string]meta, logf install.Logf) (string, error) {
	if len(v.Files) == 0 {
		return "", errors.New("release has no files")
	}
	var p project
	if err := install.GetJSON(ctx, fmt.Sprintf("%s/project/%s", api, url.PathEscape(v.ProjectID)), &p); err != nil {
		return "", err
	}
	f := v.Files[0]
	for _, ff := range v.Files {
		if ff.Primary {
			f = ff
		}
	}
	name := filepath.Base(f.Filename)
	if !strings.HasSuffix(name, ".jar") || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("unexpected file %q", f.Filename)
	}
	dir := filepath.Join(serverDir, folder(typ))
	logf("Installing %s %s", p.Title, v.VersionNumber)
	if err := install.Download(ctx, f.URL, filepath.Join(dir, name), install.Checksum{Algo: "sha512", Hex: f.Hashes.SHA512}, nil); err != nil {
		return "", err
	}
	// Replace older files from the same project.
	for file, mt := range m {
		if mt.ProjectID == v.ProjectID && file != name {
			os.Remove(filepath.Join(dir, file))
			os.Remove(filepath.Join(dir, file+".disabled"))
			delete(m, file)
		}
	}
	if replaces != "" && strings.TrimSuffix(replaces, ".disabled") != name {
		os.Remove(filepath.Join(dir, replaces))
		delete(m, strings.TrimSuffix(replaces, ".disabled"))
	}
	m[name] = meta{ProjectID: v.ProjectID, Title: p.Title, Version: v.VersionNumber}
	return p.Title, nil
}

// Update is a newer release of an installed add-on.
type Update struct {
	Version string
	v       version
}

func fileSHA512(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CheckUpdates asks Modrinth for newer releases of the installed add-ons,
// matching each jar by its hash, so files added by hand are checked too if
// they came from Modrinth. The result is keyed by Addon.Base.
func CheckUpdates(ctx context.Context, typ, mcVersion, serverDir string) (map[string]Update, error) {
	list, err := List(typ, serverDir)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	dir := filepath.Join(serverDir, folder(typ))
	byHash := map[string]string{}
	var hashes []string
	for _, a := range list {
		h, err := fileSHA512(filepath.Join(dir, a.File))
		if err != nil {
			continue
		}
		byHash[h] = a.File
		hashes = append(hashes, h)
	}
	if len(hashes) == 0 {
		return nil, nil
	}
	body := map[string]any{
		"hashes":        hashes,
		"algorithm":     "sha512",
		"loaders":       loaders(typ),
		"game_versions": []string{mcVersion},
	}
	var res map[string]version
	if err := install.PostJSON(ctx, api+"/version_files/update", body, &res); err != nil {
		return nil, err
	}
	out := map[string]Update{}
	for h, v := range res {
		file, ok := byHash[h]
		if !ok {
			continue
		}
		same := false
		for _, f := range v.Files {
			if f.Hashes.SHA512 == h {
				same = true
			}
		}
		if !same && len(v.Files) > 0 {
			out[strings.TrimSuffix(file, ".disabled")] = Update{Version: v.VersionNumber, v: v}
		}
	}
	return out, nil
}

// Apply installs u in place of file, keeping it turned off if it was off.
func Apply(ctx context.Context, typ, serverDir, file string, u Update, logf install.Logf) (string, error) {
	if err := checkFile(file); err != nil {
		return "", err
	}
	metaMu.Lock()
	defer metaMu.Unlock()
	m := loadMeta(serverDir)
	title, err := installVersion(ctx, typ, u.v, file, serverDir, m, logf)
	if err != nil {
		return "", err
	}
	if err := saveMeta(serverDir, m); err != nil {
		return title, err
	}
	if strings.HasSuffix(file, ".disabled") {
		dir := filepath.Join(serverDir, folder(typ))
		for name, mt := range m {
			if mt.ProjectID == u.v.ProjectID {
				os.Rename(filepath.Join(dir, name), filepath.Join(dir, name+".disabled"))
			}
		}
	}
	return title, nil
}

// Base is the file name without the ".disabled" suffix.
func (a Addon) Base() string { return strings.TrimSuffix(a.File, ".disabled") }

// List returns installed add-ons.
func List(typ, serverDir string) ([]Addon, error) {
	dir := filepath.Join(serverDir, folder(typ))
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	metaMu.Lock()
	m := loadMeta(serverDir)
	metaMu.Unlock()
	var out []Addon
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		enabled := strings.HasSuffix(n, ".jar")
		if !enabled && !strings.HasSuffix(n, ".jar.disabled") {
			continue
		}
		base := strings.TrimSuffix(n, ".disabled")
		a := Addon{File: n, Enabled: enabled, Title: strings.TrimSuffix(base, ".jar")}
		if mt, ok := m[base]; ok {
			a.Title, a.Version, a.ProjectID = mt.Title, mt.Version, mt.ProjectID
		}
		if info, err := e.Info(); err == nil {
			a.Size = info.Size()
		}
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b Addon) int { return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)) })
	return out, nil
}

func checkFile(name string) error {
	if name != filepath.Base(name) || !(strings.HasSuffix(name, ".jar") || strings.HasSuffix(name, ".jar.disabled")) {
		return errors.New("invalid file name")
	}
	return nil
}

// Toggle enables or disables an add-on by renaming it.
func Toggle(typ, serverDir, file string) error {
	if err := checkFile(file); err != nil {
		return err
	}
	dir := filepath.Join(serverDir, folder(typ))
	var dst string
	if strings.HasSuffix(file, ".disabled") {
		dst = strings.TrimSuffix(file, ".disabled")
	} else {
		dst = file + ".disabled"
	}
	return os.Rename(filepath.Join(dir, file), filepath.Join(dir, dst))
}

// Remove deletes an add-on.
func Remove(typ, serverDir, file string) error {
	if err := checkFile(file); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(serverDir, folder(typ), file)); err != nil {
		return err
	}
	metaMu.Lock()
	defer metaMu.Unlock()
	m := loadMeta(serverDir)
	delete(m, strings.TrimSuffix(file, ".disabled"))
	return saveMeta(serverDir, m)
}

// Note records the title and version of a jar McPanel got from somewhere
// other than Modrinth, so the list shows them instead of the file name.
func Note(serverDir, file, title, version string) error {
	if err := checkFile(file); err != nil {
		return err
	}
	metaMu.Lock()
	defer metaMu.Unlock()
	m := loadMeta(serverDir)
	m[strings.TrimSuffix(file, ".disabled")] = meta{Title: title, Version: version}
	return saveMeta(serverDir, m)
}

// Folder returns the add-on directory name for display.
func Folder(typ string) string { return folder(typ) }
