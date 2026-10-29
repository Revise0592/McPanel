// Package geyser sets up Geyser and Floodgate on Paper servers, so players
// on Bedrock Edition (phones, consoles, Windows) can join Java servers.
// Both come from GeyserMC's own downloads; Floodgate's Paper build isn't on
// Modrinth.
package geyser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Revise0592/McPanel/internal/addons"
	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/store"
)

const api = "https://download.geysermc.org/v2/projects/"

// part is one of the two plugins. prefix matches its jar whether McPanel,
// Modrinth or the user put it there, but not add-ons such as
// GeyserSkinManager.
type part struct {
	project, title, prefix string
}

var parts = []part{
	{"geyser", "Geyser", "geyser-spigot"},
	{"floodgate", "Floodgate", "floodgate-spigot"},
}

// Supported reports whether a server type can run Geyser through McPanel.
func Supported(typ string) bool { return typ == store.TypePaper }

// Status is what's installed. Each field is an addons.Addon with an empty
// File when that plugin is missing.
type Status struct {
	Geyser, Floodgate addons.Addon
}

// On reports whether Geyser is installed and turned on.
func (s Status) On() bool { return s.Geyser.File != "" && s.Geyser.Enabled }

// Find looks for Geyser and Floodgate in the server's plugins folder.
func Find(serverDir string) Status {
	var st Status
	list, _ := addons.List(store.TypePaper, serverDir)
	for _, a := range list {
		switch partOf(a.File) {
		case "geyser":
			st.Geyser = a
		case "floodgate":
			st.Floodgate = a
		}
	}
	return st
}

func partOf(file string) string {
	f := strings.ToLower(file)
	for _, p := range parts {
		if strings.HasPrefix(f, p.prefix) {
			return p.project
		}
	}
	return ""
}

type build struct {
	Version   string `json:"version"`
	Build     int    `json:"build"`
	Downloads map[string]struct {
		Name   string `json:"name"`
		SHA256 string `json:"sha256"`
	} `json:"downloads"`
}

// Install downloads the newest Geyser and Floodgate for Paper, replacing
// any older copies. It returns what was installed, e.g. "Geyser 2.12.1".
func Install(ctx context.Context, serverDir string, logf install.Logf) ([]string, error) {
	dir := filepath.Join(serverDir, addons.Folder(store.TypePaper))
	var done []string
	for _, p := range parts {
		var b build
		if err := install.GetJSON(ctx, api+p.project+"/versions/latest/builds/latest", &b); err != nil {
			return done, fmt.Errorf("%s: %w", p.title, err)
		}
		dl, ok := b.Downloads["spigot"]
		if !ok || dl.Name != filepath.Base(dl.Name) || !strings.HasSuffix(dl.Name, ".jar") {
			return done, fmt.Errorf("%s: no Paper download in build %d", p.title, b.Build)
		}
		version := fmt.Sprintf("%s (build %d)", b.Version, b.Build)
		logf("Installing %s %s", p.title, version)
		src := fmt.Sprintf("%s%s/versions/%s/builds/%d/downloads/spigot", api, p.project, url.PathEscape(b.Version), b.Build)
		if err := install.Download(ctx, src, filepath.Join(dir, dl.Name), install.Checksum{Algo: "sha256", Hex: dl.SHA256}, logf); err != nil {
			return done, fmt.Errorf("%s: %w", p.title, err)
		}
		// Older copies, including one turned off on the Plugins tab.
		list, _ := addons.List(store.TypePaper, serverDir)
		for _, a := range list {
			if partOf(a.File) == p.project && a.File != dl.Name {
				addons.Remove(store.TypePaper, serverDir, a.File)
			}
		}
		if err := addons.Note(serverDir, dl.Name, p.title, version); err != nil {
			return done, err
		}
		done = append(done, p.title+" "+b.Version)
	}
	return done, nil
}

// Remove deletes the Geyser and Floodgate jars. Their settings folders are
// kept, so turning them back on keeps any changes.
func Remove(serverDir string) error {
	list, err := addons.List(store.TypePaper, serverDir)
	if err != nil {
		return err
	}
	var errs []error
	for _, a := range list {
		if partOf(a.File) != "" {
			if err := addons.Remove(store.TypePaper, serverDir, a.File); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
