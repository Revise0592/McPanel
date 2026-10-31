// Package modpack sets up servers from Modrinth modpacks (.mrpack files).
//
// A pack is a zip with modrinth.index.json, which lists the Minecraft and
// loader versions and every mod to download, plus "overrides" (configs and
// other files for everyone) and "server-overrides" folders. Mods marked as
// not needed on servers are skipped, since many packs are mostly
// client-side.
//
// Format: https://support.modrinth.com/en/articles/8802351-modrinth-modpack-format-mrpack
package modpack

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/store"
)

// IndexFile is the pack's manifest inside the zip.
const IndexFile = "modrinth.index.json"

// Limits on what a pack may contain, so a bad file can't fill the disk.
const (
	maxIndex     = 16 << 20
	maxFiles     = 5000
	maxOverrides = 4 << 30
)

// allowedHosts are where the format lets packs download files from.
var allowedHosts = map[string]bool{
	"cdn.modrinth.com":          true,
	"github.com":                true,
	"raw.githubusercontent.com": true,
	"gitlab.com":                true,
}

// download fetches one file; tests replace it.
var download = install.Download

// File is one download listed in the pack.
type File struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       *Env              `json:"env,omitempty"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

// Env says whether a file is needed on clients and servers: "required",
// "optional" or "unsupported".
type Env struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

// ForServer reports whether the file belongs on a server.
func (f File) ForServer() bool { return f.Env == nil || f.Env.Server != "unsupported" }

// Index is modrinth.index.json.
type Index struct {
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Summary       string            `json:"summary"`
	Files         []File            `json:"files"`
	Dependencies  map[string]string `json:"dependencies"`
}

// Pack is what a server needs to know about a pack.
type Pack struct {
	Name      string
	Version   string
	Minecraft string
	Type      string // store.TypeFabric, TypeNeoForge or TypeForge
	Loader    string // the loader version the pack asks for
	// Files and Size count only what a server downloads.
	Files   int
	Size    int64
	Skipped int // client-only files left out
	// server are the files a server gets; client the ones left out.
	server, client []File
	// rescued maps client-only files brought back for the server to the
	// server files that need them.
	rescued map[string][]string
}

// ErrNotPack means the file isn't a Modrinth modpack.
var ErrNotPack = errors.New("that isn't a Modrinth modpack (.mrpack)")

// Read opens a pack and checks it can run on a server McPanel supports.
func Read(path string) (Pack, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return Pack{}, ErrNotPack
	}
	defer z.Close()
	var idx Index
	found := false
	for _, f := range z.File {
		if f.Name != IndexFile {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return Pack{}, err
		}
		err = json.NewDecoder(io.LimitReader(r, maxIndex)).Decode(&idx)
		r.Close()
		if err != nil {
			return Pack{}, fmt.Errorf("the pack's %s can't be read: %w", IndexFile, err)
		}
		found = true
	}
	if !found {
		return Pack{}, ErrNotPack
	}
	return check(idx)
}

func check(idx Index) (Pack, error) {
	if idx.FormatVersion != 1 || idx.Game != "minecraft" {
		return Pack{}, errors.New("this pack uses a format McPanel doesn't know")
	}
	if len(idx.Files) > maxFiles {
		return Pack{}, fmt.Errorf("the pack lists %d files, more than McPanel accepts", len(idx.Files))
	}
	p := Pack{Name: idx.Name, Version: idx.VersionID, Minecraft: idx.Dependencies["minecraft"]}
	if p.Minecraft == "" {
		return Pack{}, errors.New("the pack doesn't say which Minecraft version it's for")
	}
	deps := idx.Dependencies
	switch {
	case deps["neoforge"] != "":
		p.Type, p.Loader = store.TypeNeoForge, deps["neoforge"]
	case deps["forge"] != "":
		p.Type, p.Loader = store.TypeForge, strings.TrimPrefix(deps["forge"], p.Minecraft+"-")
	case deps["fabric-loader"] != "":
		p.Type, p.Loader = store.TypeFabric, deps["fabric-loader"]
	case deps["quilt-loader"] != "":
		return Pack{}, errors.New("this pack uses the Quilt loader, which McPanel can't run yet")
	default:
		return Pack{}, errors.New("this pack doesn't use Fabric, NeoForge or Forge")
	}
	for _, f := range idx.Files {
		if !f.ForServer() {
			p.client = append(p.client, f)
			continue
		}
		if err := safe(f); err != nil {
			return Pack{}, err
		}
		p.server = append(p.server, f)
	}
	p.count()
	return p, nil
}

func (p *Pack) count() {
	p.Files, p.Size, p.Skipped = len(p.server), 0, len(p.client)
	for _, f := range p.server {
		p.Size += f.FileSize
	}
}

// safe checks a file stays in the server folder and comes from an allowed
// site.
func safe(f File) error {
	if _, err := localPath(f.Path); err != nil {
		return err
	}
	_, err := source(f)
	return err
}

// localPath checks a path from the pack stays inside the server folder.
func localPath(p string) (string, error) {
	fp := filepath.FromSlash(p)
	if p == "" || strings.Contains(p, "\\") || !filepath.IsLocal(fp) {
		return "", fmt.Errorf("the pack has an unsafe file path: %q", p)
	}
	return fp, nil
}

// source picks a download address the format allows.
func source(f File) (string, error) {
	for _, d := range f.Downloads {
		u, err := url.Parse(d)
		if err == nil && u.Scheme == "https" && allowedHosts[u.Hostname()] {
			return d, nil
		}
	}
	return "", fmt.Errorf("the pack downloads %s from a site modpacks aren't allowed to use", f.Path)
}

func checksum(f File) install.Checksum {
	if h := f.Hashes["sha512"]; h != "" {
		return install.Checksum{Algo: "sha512", Hex: strings.ToLower(h)}
	}
	return install.Checksum{Algo: "sha1", Hex: strings.ToLower(f.Hashes["sha1"])}
}

// Apply downloads the pack's server files into dir, then copies its
// overrides over them. progress follows the bytes downloaded.
func Apply(ctx context.Context, packPath string, p Pack, dir string, progress func(done, total int64), logf install.Logf) error {
	p = addRequired(ctx, p, logf)
	files := p.server
	logf("Downloading %d mods and files for %s %s (%d client-only ones left out)", len(files), p.Name, p.Version, p.Skipped)
	var done int64
	var progressMu sync.Mutex
	// Each download reports to us, not to the install's progress bar.
	quiet := install.WithProgress(ctx, nil)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan File)
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				rel, _ := localPath(f.Path)
				src, _ := source(f)
				if err := download(quiet, src, filepath.Join(dir, rel), checksum(f), nil); err != nil {
					errOnce.Do(func() {
						firstErr = fmt.Errorf("download %s: %w", f.Path, err)
						cancel()
					})
					continue
				}
				// One at a time and in order, so the bar never goes back.
				progressMu.Lock()
				done += f.FileSize
				if progress != nil {
					progress(done, p.Size)
				}
				progressMu.Unlock()
			}
		}()
	}
	for _, f := range files {
		select {
		case jobs <- f:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p = dropUnneeded(p, dir, logf)
	logf("Copying the pack's settings files")
	return overrides(packPath, dir)
}

// overrides copies "overrides" and then "server-overrides" into dir.
func overrides(packPath, dir string) error {
	z, err := zip.OpenReader(packPath)
	if err != nil {
		return err
	}
	defer z.Close()
	var total int64
	for _, prefix := range []string{"overrides/", "server-overrides/"} {
		for _, f := range z.File {
			name, ok := strings.CutPrefix(f.Name, prefix)
			if !ok || name == "" || strings.HasSuffix(name, "/") {
				continue
			}
			rel, err := localPath(name)
			if err != nil {
				return err
			}
			total += int64(f.UncompressedSize64)
			if total > maxOverrides {
				return errors.New("the pack's settings files are too big")
			}
			if err := extract(f, filepath.Join(dir, rel)); err != nil {
				return err
			}
		}
	}
	return nil
}

func extract(f *zip.File, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	r, err := f.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(r, maxOverrides)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
