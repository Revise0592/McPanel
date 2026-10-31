package modpack

import (
	"archive/zip"
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/store"
)

// On NeoForge and Forge a mod made only for players' computers can crash a
// server, unlike on Fabric, which skips such mods. So a file brought back
// because Modrinth lists it as required (see addRequired) is checked
// against what the mods themselves declare in their mods.toml, which is
// what the loader goes by. It's removed again only when no mod that needs
// it declares it as mandatory on servers; when that can't be told, it stays.

// modsToml is what McPanel reads from a NeoForge or Forge mod's metadata.
type modsToml struct {
	IDs  []string  // the mods in the jar
	Deps []modsDep // what they depend on
}

type modsDep struct {
	ModID     string
	Mandatory bool
	Side      string // BOTH, CLIENT or SERVER
}

// neededOnServer reports whether the dependency must be there for a server
// to start.
func (d modsDep) neededOnServer() bool { return d.Mandatory && d.Side != "CLIENT" }

// metaFiles are where NeoForge (1.20.5 and later) and Forge keep mods.toml.
var metaFiles = []string{"META-INF/neoforge.mods.toml", "META-INF/mods.toml"}

// readModsToml reads a jar's mods.toml. ok is false if there isn't one.
func readModsToml(jar string) (m modsToml, ok bool) {
	z, err := zip.OpenReader(jar)
	if err != nil {
		return m, false
	}
	defer z.Close()
	for _, name := range metaFiles {
		for _, f := range z.File {
			if f.Name != name {
				continue
			}
			r, err := f.Open()
			if err != nil {
				return m, false
			}
			defer r.Close()
			return parseModsToml(io.LimitReader(r, 1<<20)), true
		}
	}
	return m, false
}

// parseModsToml reads the [[mods]] and [[dependencies.*]] tables. It
// understands just the plain key = value lines those tables use, and skips
// multi-line strings such as descriptions.
func parseModsToml(r io.Reader) modsToml {
	var m modsToml
	section := ""
	var dep *modsDep
	var depType string
	hasType := false
	flush := func() {
		if dep == nil {
			return
		}
		if hasType {
			// NeoForge: type = "required" | "optional" | ...
			dep.Mandatory = strings.EqualFold(depType, "required")
		}
		if dep.Side == "" {
			dep.Side = "BOTH"
		}
		if dep.ModID != "" {
			m.Deps = append(m.Deps, *dep)
		}
		dep, depType, hasType = nil, "", false
	}
	inMultiline := ""
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if inMultiline != "" {
			if strings.Contains(line, inMultiline) {
				inMultiline = ""
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			flush()
			section = strings.Trim(strings.SplitN(line, "#", 2)[0], "[] \t")
			if strings.HasPrefix(section, "dependencies.") && strings.HasPrefix(line, "[[") {
				// Missing keys count as mandatory, which keeps the file.
				dep = &modsDep{Mandatory: true}
			}
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		for _, q := range []string{`"""`, `'''`} {
			if strings.HasPrefix(val, q) {
				if !strings.Contains(val[3:], q) {
					inMultiline = q
				}
				val = ""
			}
		}
		val = tomlValue(val)
		switch {
		case section == "mods" && key == "modId":
			m.IDs = append(m.IDs, val)
		case dep != nil && key == "modId":
			dep.ModID = val
		case dep != nil && key == "mandatory":
			dep.Mandatory = val == "true"
		case dep != nil && key == "type":
			depType, hasType = val, true
		case dep != nil && key == "side":
			dep.Side = strings.ToUpper(val)
		}
	}
	flush()
	return m
}

// tomlValue reads a quoted string or bare value, dropping a trailing comment.
func tomlValue(v string) string {
	if v == "" {
		return ""
	}
	if q := v[0]; q == '"' || q == '\'' {
		if end := strings.IndexByte(v[1:], q); end >= 0 {
			return v[1 : end+1]
		}
		return strings.Trim(v, `"'`)
	}
	if i := strings.Index(v, "#"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// dropUnneeded removes rescued files that, by the mods' own metadata, no
// server mod needs. It only acts on NeoForge and Forge packs.
func dropUnneeded(p Pack, dir string, logf install.Logf) Pack {
	if len(p.rescued) == 0 || (p.Type != store.TypeNeoForge && p.Type != store.TypeForge) {
		return p
	}
	meta := map[string]*modsToml{}
	read := func(path string) *modsToml {
		if m, seen := meta[path]; seen {
			return m
		}
		var m *modsToml
		if rel, err := localPath(path); err == nil {
			if t, ok := readModsToml(filepath.Join(dir, rel)); ok {
				m = &t
			}
		}
		meta[path] = m
		return m
	}
	// A rescued file stays while some file that needs it is kept and
	// either declares it as mandatory on servers or can't be read.
	keep := map[string]bool{}
	for path := range p.rescued {
		keep[path] = true
	}
	for changed := true; changed; {
		changed = false
		for path, needers := range p.rescued {
			if !keep[path] {
				continue
			}
			own := read(path)
			if own == nil || len(own.IDs) == 0 {
				continue // can't tell what it is: keep it
			}
			needed := false
			for _, n := range needers {
				if _, rescued := p.rescued[n]; rescued && !keep[n] {
					continue // needed only by a file that was dropped
				}
				nm := read(n)
				if nm == nil {
					needed = true // can't read the mod that needs it: keep it
					break
				}
				for _, d := range nm.Deps {
					if d.neededOnServer() && containsFold(own.IDs, d.ModID) {
						needed = true
					}
				}
				if needed {
					break
				}
			}
			if !needed {
				keep[path] = false
				changed = true
			}
		}
	}
	for path, k := range keep {
		if k {
			continue
		}
		if rel, err := localPath(path); err == nil {
			os.Remove(filepath.Join(dir, rel))
		}
		logf("Left out %s after all: the mods that need it only need it on players' computers", path)
	}
	return p
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
