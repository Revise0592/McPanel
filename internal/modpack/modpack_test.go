package modpack

import (
	"archive/zip"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// noLookup stands in for Modrinth, which tests don't call: it knows no files.
func noLookup(context.Context, []string) (map[string]fileVersion, error) {
	return map[string]fileVersion{}, nil
}

func TestMain(m *testing.M) {
	lookupFiles = noLookup
	os.Exit(m.Run())
}

func sum(b string) string {
	h := sha512.Sum512([]byte(b))
	return hex.EncodeToString(h[:])
}

// writePack makes a .mrpack with the given index and extra zip entries.
func writePack(t *testing.T, idx Index, extra map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pack.mrpack")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create(IndexFile)
	json.NewEncoder(w).Encode(idx)
	for name, body := range extra {
		w, _ := z.Create(name)
		w.Write([]byte(body))
	}
	z.Close()
	f.Close()
	return p
}

func file(path, body, server string) File {
	f := File{Path: path, Hashes: map[string]string{"sha512": sum(body)}, Downloads: []string{"https://cdn.modrinth.com/data/x/" + filepath.Base(path)}, FileSize: int64(len(body))}
	if server != "" {
		f.Env = &Env{Client: "required", Server: server}
	}
	return f
}

func index(files ...File) Index {
	return Index{FormatVersion: 1, Game: "minecraft", VersionID: "2.0", Name: "Test Pack",
		Dependencies: map[string]string{"minecraft": "1.21.1", "neoforge": "21.1.77"}, Files: files}
}

func TestReadAndApply(t *testing.T) {
	bodies := map[string]string{
		"https://cdn.modrinth.com/data/x/create.jar":  "create mod",
		"https://cdn.modrinth.com/data/x/sodium.jar":  "client only",
		"https://cdn.modrinth.com/data/x/jei.jar":     "jei",
		"https://cdn.modrinth.com/data/x/serveronly":  "server tool",
		"https://cdn.modrinth.com/data/x/options.txt": "should not exist",
	}
	var mu sync.Mutex
	var fetched []string
	download = func(ctx context.Context, url, dst string, sum install.Checksum, logf install.Logf) error {
		mu.Lock()
		fetched = append(fetched, url)
		mu.Unlock()
		if sum.Algo != "sha512" || sum.Hex == "" {
			t.Errorf("%s: no checksum", url)
		}
		os.MkdirAll(filepath.Dir(dst), 0o755)
		return os.WriteFile(dst, []byte(bodies[url]), 0o644)
	}
	defer func() { download = install.Download }()

	path := writePack(t, index(
		file("mods/create.jar", "create mod", "required"),
		file("mods/sodium.jar", "client only", "unsupported"),
		file("mods/jei.jar", "jei", "optional"),
		file("tools/serveronly", "server tool", ""),
	), map[string]string{
		"overrides/config/create.toml":        "client and server",
		"overrides/config/shared.toml":        "from overrides",
		"server-overrides/config/shared.toml": "from server-overrides",
		"client-overrides/options.txt":        "client only",
	})
	p, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != store.TypeNeoForge || p.Loader != "21.1.77" || p.Minecraft != "1.21.1" || p.Files != 3 || p.Skipped != 1 || p.Name != "Test Pack" || p.Version != "2.0" {
		t.Fatalf("pack = %+v", p)
	}

	dir := t.TempDir()
	var last, total int64
	err = Apply(context.Background(), path, p, dir, func(d, tot int64) { last, total = d, tot }, func(string, ...any) {})
	if err != nil {
		t.Fatal(err)
	}
	if last != total || total != p.Size {
		t.Errorf("progress ended at %d of %d (size %d)", last, total, p.Size)
	}
	want := map[string]string{
		"mods/create.jar":    "create mod",
		"mods/jei.jar":       "jei",
		"tools/serveronly":   "server tool",
		"config/create.toml": "client and server",
		"config/shared.toml": "from server-overrides",
	}
	for rel, body := range want {
		b, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil || string(b) != body {
			t.Errorf("%s = %q, %v; want %q", rel, b, err, body)
		}
	}
	for _, gone := range []string{"mods/sodium.jar", "options.txt"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s was installed on the server", gone)
		}
	}
	if len(fetched) != 3 {
		t.Errorf("fetched %v", fetched)
	}
}

func TestApplyStopsOnFailedDownload(t *testing.T) {
	download = func(ctx context.Context, url, dst string, sum install.Checksum, logf install.Logf) error {
		if strings.HasSuffix(url, "bad.jar") {
			return errors.New("checksum mismatch")
		}
		os.MkdirAll(filepath.Dir(dst), 0o755)
		return os.WriteFile(dst, nil, 0o644)
	}
	defer func() { download = install.Download }()
	path := writePack(t, index(file("mods/good.jar", "", ""), file("mods/bad.jar", "", "")), nil)
	p, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	err = Apply(context.Background(), path, p, t.TempDir(), nil, func(string, ...any) {})
	if err == nil || !strings.Contains(err.Error(), "bad.jar") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadRejects(t *testing.T) {
	bad := func(mut func(*Index)) Index {
		i := index(file("mods/a.jar", "a", ""))
		mut(&i)
		return i
	}
	cases := map[string]Index{
		"escaping path":  bad(func(i *Index) { i.Files[0].Path = "../../etc/cron.d/x" }),
		"absolute path":  bad(func(i *Index) { i.Files[0].Path = "/etc/passwd" }),
		"backslash path": bad(func(i *Index) { i.Files[0].Path = `mods\..\..\x` }),
		"other host":     bad(func(i *Index) { i.Files[0].Downloads = []string{"https://evil.example/a.jar"} }),
		"plain http":     bad(func(i *Index) { i.Files[0].Downloads = []string{"http://cdn.modrinth.com/a.jar"} }),
		"quilt":          bad(func(i *Index) { i.Dependencies = map[string]string{"minecraft": "1.21.1", "quilt-loader": "0.26"} }),
		"no minecraft":   bad(func(i *Index) { delete(i.Dependencies, "minecraft") }),
		"no loader":      bad(func(i *Index) { i.Dependencies = map[string]string{"minecraft": "1.21.1"} }),
		"format 2":       bad(func(i *Index) { i.FormatVersion = 2 }),
	}
	for name, idx := range cases {
		if _, err := Read(writePack(t, idx, nil)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A client-only file may point anywhere: it's never downloaded.
	ok := index(file("mods/a.jar", "a", ""), File{Path: "../x", Downloads: []string{"https://evil.example/x"}, Env: &Env{Server: "unsupported"}})
	if _, err := Read(writePack(t, ok, nil)); err != nil {
		t.Errorf("client-only file was checked: %v", err)
	}

	notZip := filepath.Join(t.TempDir(), "x.mrpack")
	os.WriteFile(notZip, []byte("hello"), 0o644)
	if _, err := Read(notZip); !errors.Is(err, ErrNotPack) {
		t.Errorf("not a zip: %v", err)
	}
}

func TestOverridesCantEscape(t *testing.T) {
	path := writePack(t, index(), map[string]string{"overrides/../../evil.txt": "x"})
	p, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := Apply(context.Background(), path, p, dir, nil, func(string, ...any) {}); err == nil {
		t.Fatal("an override outside the server folder was accepted")
	}
}

func TestForgeLoaderVersion(t *testing.T) {
	i := index()
	i.Dependencies = map[string]string{"minecraft": "1.20.1", "forge": "1.20.1-47.3.0"}
	p, err := check(i)
	if err != nil || p.Type != store.TypeForge || p.Loader != "47.3.0" {
		t.Fatalf("got %+v, %v", p, err)
	}
}

func TestClientOnlyDependencyIsInstalled(t *testing.T) {
	var mu sync.Mutex
	var got []string
	download = func(ctx context.Context, url, dst string, sum install.Checksum, logf install.Logf) error {
		mu.Lock()
		got = append(got, filepath.Base(dst))
		mu.Unlock()
		os.MkdirAll(filepath.Dir(dst), 0o755)
		return os.WriteFile(dst, nil, 0o644)
	}
	defer func() { download = install.Download }()
	type dep = struct {
		ProjectID      string `json:"project_id"`
		DependencyType string `json:"dependency_type"`
	}
	// Chipped needs Athena, which needs Library; the pack marks Athena and
	// Library client-only, the way Homestead does. Sodium really is.
	lookupFiles = func(ctx context.Context, hashes []string) (map[string]fileVersion, error) {
		return map[string]fileVersion{
			sum("chipped"): {Name: "Chipped 3.0.7", ProjectID: "chipped", Dependencies: []dep{{"athena", "required"}, {"sodium", "optional"}}},
			sum("athena"):  {Name: "Athena 3.1.2", ProjectID: "athena", Dependencies: []dep{{"library", "required"}}},
			sum("library"): {Name: "Library", ProjectID: "library"},
			sum("sodium"):  {Name: "Sodium", ProjectID: "sodium"},
		}, nil
	}
	defer func() { lookupFiles = noLookup }()

	path := writePack(t, index(
		file("mods/chipped.jar", "chipped", "required"),
		file("mods/athena.jar", "athena", "unsupported"),
		file("mods/library.jar", "library", "unsupported"),
		file("mods/sodium.jar", "sodium", "unsupported"),
	), nil)
	p, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }
	if err := Apply(context.Background(), path, p, t.TempDir(), nil, logf); err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"athena.jar", "chipped.jar", "library.jar"}) {
		t.Errorf("downloaded %v", got)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "mods/athena.jar: the pack marks it as only for players' computers, but Chipped 3.0.7 needs it") {
		t.Errorf("logs: %v", logs)
	}

	// Without Modrinth, the pack's own list is used.
	got = nil
	lookupFiles = func(context.Context, []string) (map[string]fileVersion, error) { return nil, errors.New("offline") }
	if err := Apply(context.Background(), path, p, t.TempDir(), nil, logf); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("offline: downloaded %v", got)
	}
}
