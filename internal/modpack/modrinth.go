package modpack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/store"
)

const api = "https://api.modrinth.com/v2"

// loaderTypes maps Modrinth loader names to the server types that run them.
var loaderTypes = map[string]string{
	"neoforge": store.TypeNeoForge,
	"forge":    store.TypeForge,
	"fabric":   store.TypeFabric,
}

// Hit is a modpack in search results.
type Hit struct {
	ProjectID   string   `json:"project_id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	IconURL     string   `json:"icon_url"`
	Downloads   int      `json:"downloads"`
	ServerSide  string   `json:"server_side"`
	Categories  []string `json:"categories"`
	Versions    []string `json:"versions"`
}

// Loaders are the loaders a hit says it uses, as server type labels.
func (h Hit) Loaders() string {
	var out []string
	for _, c := range h.Categories {
		if t, ok := loaderTypes[c]; ok {
			out = append(out, store.TypeLabel(t))
		}
	}
	return strings.Join(out, ", ")
}

// Latest is the newest Minecraft version the pack has a release for.
func (h Hit) Latest() string {
	if len(h.Versions) == 0 {
		return ""
	}
	vs := slices.Clone(h.Versions)
	install.SortVersionsDesc(vs)
	return vs[0]
}

// Search finds modpacks that run on servers with a loader McPanel supports,
// most downloaded first when query is empty.
func Search(ctx context.Context, query string) ([]Hit, error) {
	facets, _ := json.Marshal([][]string{
		{"project_type:modpack"},
		{"categories:fabric", "categories:neoforge", "categories:forge"},
		{"server_side:required", "server_side:optional"},
	})
	q := url.Values{}
	q.Set("query", query)
	q.Set("facets", string(facets))
	q.Set("limit", "30")
	if query == "" {
		q.Set("index", "downloads")
	}
	var res struct {
		Hits []Hit `json:"hits"`
	}
	if err := install.GetJSON(ctx, api+"/search?"+q.Encode(), &res); err != nil {
		return nil, err
	}
	// The server side facet doesn't catch every client-only pack.
	hits := slices.DeleteFunc(res.Hits, func(h Hit) bool { return h.ServerSide == "unsupported" || h.Loaders() == "" })
	if len(hits) > 20 {
		hits = hits[:20]
	}
	return hits, nil
}

// Project is a modpack's page on Modrinth.
type Project struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
	ProjectType string `json:"project_type"`
	ServerSide  string `json:"server_side"`
}

// Version is a release of a modpack that McPanel can install.
type Version struct {
	ID            string    `json:"id"`
	VersionNumber string    `json:"version_number"`
	Name          string    `json:"name"`
	GameVersions  []string  `json:"game_versions"`
	Loaders       []string  `json:"loaders"`
	VersionType   string    `json:"version_type"`
	Published     time.Time `json:"date_published"`
	Files         []struct {
		URL      string `json:"url"`
		Filename string `json:"filename"`
		Primary  bool   `json:"primary"`
		Size     int64  `json:"size"`
		Hashes   struct {
			SHA512 string `json:"sha512"`
		} `json:"hashes"`
	} `json:"files"`
	// Type is the server type that runs it.
	Type string `json:"-"`
	// File is its .mrpack.
	FileURL, FileSHA512 string
	FileSize            int64
}

// Label describes the release in a list.
func (v Version) Label() string {
	s := v.VersionNumber
	if len(v.GameVersions) > 0 {
		s += " · Minecraft " + strings.Join(v.GameVersions, ", ")
	}
	s += " · " + store.TypeLabel(v.Type)
	if v.VersionType != "release" && v.VersionType != "" {
		s += " (" + v.VersionType + ")"
	}
	return s
}

// validID reports whether s can be a Modrinth project ID or slug.
func validID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// GetProject looks up a modpack by ID or slug.
func GetProject(ctx context.Context, id string) (Project, error) {
	var p Project
	if !validID(id) {
		return p, errors.New("not a Modrinth project")
	}
	if err := install.GetJSON(ctx, api+"/project/"+url.PathEscape(id), &p); err != nil {
		return p, err
	}
	if p.ProjectType != "modpack" {
		return p, errors.New("that Modrinth project isn't a modpack")
	}
	return p, nil
}

// Versions lists a modpack's releases that McPanel can run, newest first.
func Versions(ctx context.Context, projectID string) ([]Version, error) {
	if !validID(projectID) {
		return nil, errors.New("not a Modrinth project")
	}
	var all []Version
	if err := install.GetJSON(ctx, fmt.Sprintf("%s/project/%s/version", api, url.PathEscape(projectID)), &all); err != nil {
		return nil, err
	}
	var out []Version
	for _, v := range all {
		for _, l := range []string{"neoforge", "forge", "fabric"} {
			if slices.Contains(v.Loaders, l) {
				v.Type = loaderTypes[l]
				break
			}
		}
		if v.Type == "" {
			continue
		}
		for _, f := range v.Files {
			if strings.HasSuffix(f.Filename, ".mrpack") && (f.Primary || v.FileURL == "") {
				v.FileURL, v.FileSHA512, v.FileSize = f.URL, f.Hashes.SHA512, f.Size
			}
		}
		if v.FileURL != "" {
			out = append(out, v)
		}
	}
	slices.SortStableFunc(out, func(a, b Version) int { return b.Published.Compare(a.Published) })
	return out, nil
}

// Download fetches a release's .mrpack to dst.
func Download(ctx context.Context, v Version, dst string, logf install.Logf) error {
	u, err := url.Parse(v.FileURL)
	if err != nil || u.Scheme != "https" || !allowedHosts[u.Hostname()] {
		return errors.New("the modpack's download address isn't one McPanel trusts")
	}
	return install.Download(ctx, v.FileURL, dst, install.Checksum{Algo: "sha512", Hex: v.FileSHA512}, logf)
}
