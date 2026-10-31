package modpack

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/store"
)

const forgeToml = `modLoader="javafml" #mandatory
loaderVersion="[47,)"
license="MIT"

[[mods]] #mandatory
modId="wearablelanterns"
version="1.0.1"
displayName="Wearable Lanterns"
description='''
Wear your lanterns.
[[dependencies.fake]]
modId="notreal"
'''

[[dependencies.wearablelanterns]]
    modId="forge"
    mandatory=true
    versionRange="[47,)"
    ordering="NONE"
    side="BOTH"
[[dependencies.wearablelanterns]]
    modId="lambdynlights" # dynamic light
    mandatory=true
    versionRange="*"
    side="CLIENT"
[[dependencies.wearablelanterns]]
    modId='accessories'
    mandatory=false
    side="BOTH"
`

const neoToml = `modLoader = "javafml"
loaderVersion = "[4,)"

[[mods]]
modId = "chipped"

[[mods]]
modId = "chipped_extras"

[[dependencies.chipped]]
modId = "athena"
type = "required"
side = "BOTH"

[[dependencies.chipped]]
modId = "jei"
type = "optional"

[[dependencies.chipped]]
modId = "resourcefullib"
`

func TestParseModsToml(t *testing.T) {
	f := parseModsToml(strings.NewReader(forgeToml))
	if !slices.Equal(f.IDs, []string{"wearablelanterns"}) {
		t.Errorf("forge ids = %v", f.IDs)
	}
	want := []modsDep{{"forge", true, "BOTH"}, {"lambdynlights", true, "CLIENT"}, {"accessories", false, "BOTH"}}
	if !slices.Equal(f.Deps, want) {
		t.Errorf("forge deps = %+v", f.Deps)
	}
	n := parseModsToml(strings.NewReader(neoToml))
	if !slices.Equal(n.IDs, []string{"chipped", "chipped_extras"}) {
		t.Errorf("neoforge ids = %v", n.IDs)
	}
	wantN := []modsDep{{"athena", true, "BOTH"}, {"jei", false, "BOTH"}, {"resourcefullib", true, "BOTH"}}
	if !slices.Equal(n.Deps, wantN) {
		t.Errorf("neoforge deps = %+v", n.Deps)
	}
	if !n.Deps[0].neededOnServer() || f.Deps[1].neededOnServer() || f.Deps[2].neededOnServer() {
		t.Error("neededOnServer is wrong")
	}
}

// jarWith returns a jar holding a mods.toml, or no metadata when toml is "".
func jarWith(toml string) []byte {
	var b strings.Builder
	z := zip.NewWriter(&b)
	if toml != "" {
		w, _ := z.Create("META-INF/neoforge.mods.toml")
		w.Write([]byte(toml))
	}
	w, _ := z.Create("x.class")
	w.Write([]byte("x"))
	z.Close()
	return []byte(b.String())
}

func modToml(id string, deps ...string) string {
	s := fmt.Sprintf("[[mods]]\nmodId=%q\n", id)
	for _, d := range deps {
		name, side, _ := strings.Cut(d, ":")
		s += fmt.Sprintf("[[dependencies.%s]]\nmodId=%q\ntype=\"required\"\nside=%q\n", id, name, side)
	}
	return s
}

func TestDropUnneededOnNeoForge(t *testing.T) {
	// "lanterns" is on servers. Modrinth says it requires every other
	// mod here, which the pack marks client-only. Its own mods.toml says:
	// "library" on both sides, "dynlights" on clients only, nothing about
	// "shaders". "dynlights" in turn needs "glhelper". "mystery" has no
	// mods.toml, so it can't be told apart and stays.
	jars := map[string]string{
		"lanterns":  modToml("lanterns", "library:BOTH", "dynlights:CLIENT", "mystery:BOTH"),
		"library":   modToml("library"),
		"dynlights": modToml("dynlights", "glhelper:BOTH"),
		"glhelper":  modToml("glhelper"),
		"shaders":   modToml("shaders"),
		"mystery":   "",
	}
	var mu sync.Mutex
	download = func(ctx context.Context, url, dst string, sum install.Checksum, logf install.Logf) error {
		mu.Lock()
		defer mu.Unlock()
		os.MkdirAll(filepath.Dir(dst), 0o755)
		return os.WriteFile(dst, jarWith(jars[strings.TrimSuffix(filepath.Base(dst), ".jar")]), 0o644)
	}
	defer func() { download = install.Download }()
	type dep = struct {
		ProjectID      string `json:"project_id"`
		DependencyType string `json:"dependency_type"`
	}
	lookupFiles = func(context.Context, []string) (map[string]fileVersion, error) {
		return map[string]fileVersion{
			sum("lanterns"):  {Name: "Lanterns", ProjectID: "lanterns", Dependencies: []dep{{"library", "required"}, {"dynlights", "required"}, {"shaders", "required"}, {"mystery", "required"}}},
			sum("library"):   {Name: "Library", ProjectID: "library"},
			sum("dynlights"): {Name: "Dynamic lights", ProjectID: "dynlights", Dependencies: []dep{{"glhelper", "required"}}},
			sum("glhelper"):  {Name: "GL helper", ProjectID: "glhelper"},
			sum("shaders"):   {Name: "Shaders", ProjectID: "shaders"},
			sum("mystery"):   {Name: "Mystery", ProjectID: "mystery"},
		}, nil
	}
	defer func() { lookupFiles = noLookup }()

	files := []File{file("mods/lanterns.jar", "lanterns", "required")}
	for _, n := range []string{"library", "dynlights", "glhelper", "shaders", "mystery"} {
		files = append(files, file("mods/"+n+".jar", n, "unsupported"))
	}
	for _, typ := range []string{store.TypeNeoForge, store.TypeFabric} {
		idx := index(files...)
		if typ == store.TypeFabric {
			idx.Dependencies = map[string]string{"minecraft": "1.21.1", "fabric-loader": "0.16.0"}
		}
		path := writePack(t, idx, nil)
		p, err := Read(path)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		var logs []string
		if err := Apply(context.Background(), path, p, dir, nil, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }); err != nil {
			t.Fatal(err)
		}
		var have []string
		entries, _ := os.ReadDir(filepath.Join(dir, "mods"))
		for _, e := range entries {
			have = append(have, strings.TrimSuffix(e.Name(), ".jar"))
		}
		want := []string{"lanterns", "library", "mystery"}
		if typ == store.TypeFabric {
			// Fabric skips client-only mods by itself, so nothing is removed.
			want = []string{"dynlights", "glhelper", "lanterns", "library", "mystery", "shaders"}
		}
		if !slices.Equal(have, want) {
			t.Errorf("%s: mods = %v, want %v\n%s", typ, have, want, strings.Join(logs, "\n"))
		}
		if typ == store.TypeNeoForge && !strings.Contains(strings.Join(logs, "\n"), "Left out mods/dynlights.jar after all") {
			t.Errorf("no note about leaving dynlights out:\n%s", strings.Join(logs, "\n"))
		}
	}
}
