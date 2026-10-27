package importer

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/Revise0592/McPanel/internal/store"
)

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// levelDat builds a tiny gzipped level.dat holding a Version.Name tag.
func levelDat(version string) string {
	var raw bytes.Buffer
	raw.WriteString("\x0a\x00\x00\x0a\x00\x04Data")
	raw.WriteString("\x0a\x00\x07Version")
	raw.WriteString("\x08\x00\x04Name")
	raw.Write([]byte{0, byte(len(version))})
	raw.WriteString(version)
	raw.WriteString("\x00\x00\x00")
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	gz.Write(raw.Bytes())
	gz.Close()
	return out.String()
}

func TestDetectPaperInWrapperFolder(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{
		"MyServer/server.properties":       "motd=Friends SMP\nserver-port=25570\n",
		"MyServer/config/paper-global.yml": "",
		"MyServer/version_history.json":    `{"currentVersion":"1.21.4-232-12d8fe0 (MC: 1.21.4)"}`,
		"MyServer/world/level.dat":         levelDat("1.21.4"),
		"MyServer/plugins/EssentialsX.jar": "",
	})
	f, err := Detect(base, "upload")
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != store.TypePaper || f.Version != "1.21.4" || f.Name != "Friends SMP" || f.Port != 25570 || f.Addons != 1 {
		t.Errorf("got %+v", f)
	}
	if filepath.Base(f.Root) != "MyServer" || len(f.Worlds) != 1 {
		t.Errorf("root/worlds wrong: %s %v", f.Root, f.Worlds)
	}
}

func TestDetectSpigotNote(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{"server.properties": "", "spigot.yml": "", "logs/latest.log": "[10:00:00] [Server thread/INFO]: Starting minecraft server version 1.20.1\n"})
	f, err := Detect(base, "upload")
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != store.TypePaper || f.Version != "1.20.1" || len(f.Notes) == 0 || f.Name != "upload" {
		t.Errorf("got %+v", f)
	}
}

func TestDetectVanillaFromJar(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{"server.properties": "motd=A Minecraft Server\n"})
	jf, _ := os.Create(filepath.Join(base, "minecraft_server.jar"))
	zw := zip.NewWriter(jf)
	w, _ := zw.Create("version.json")
	w.Write([]byte(`{"id":"1.19.4","name":"1.19.4"}`))
	zw.Close()
	jf.Close()
	f, err := Detect(base, "old box")
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != store.TypeVanilla || f.Version != "1.19.4" || f.Name != "old box" {
		t.Errorf("got %+v", f)
	}
}

func TestDetectFabric(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{"server.properties": "", "fabric-server-launcher.properties": "serverJar=server.jar\n", "mods/lithium.jar": "", "mods/fabric-api.jar": ""})
	f, err := Detect(base, "x")
	if err != nil || f.Type != store.TypeFabric || f.Addons != 2 {
		t.Errorf("got %+v, %v", f, err)
	}
}

func TestDetectBedrock(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{"server.properties": "server-name=Family Realm\nserver-port=19140\n", "worlds/Survival/db/CURRENT": "", "worlds/Survival/level.dat": ""})
	f, err := Detect(base, "x")
	if err != nil || f.Type != store.TypeBedrock || f.Name != "Family Realm" || f.Port != 19140 || len(f.Worlds) != 1 {
		t.Errorf("got %+v, %v", f, err)
	}
}

func TestDetectRefusesForge(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{"server.properties": "", "libraries/net/minecraftforge/forge/x.jar": ""})
	if _, err := Detect(base, "x"); err == nil {
		t.Error("Forge server was accepted")
	}
	neo := t.TempDir()
	write(t, neo, map[string]string{"server.properties": "", "libraries/net/neoforged/neoforge/21.1/x.jar": ""})
	if _, err := Detect(neo, "x"); err == nil {
		t.Error("NeoForge server was accepted")
	}
}

func TestPaperWithNeoForgedToolingIsNotForge(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{"server.properties": "", "bukkit.yml": "", "config/paper-global.yml": "",
		"libraries/net/neoforged/srgutils/1.0.9/srgutils.jar": "", "libraries/net/neoforged/AutoRenamingTool/2.0.3/a.jar": ""})
	f, err := Detect(base, "x")
	if err != nil || f.Type != store.TypePaper {
		t.Errorf("got %+v, %v", f, err)
	}
}

func TestDetectJavaWorldOnly(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{"My World/level.dat": levelDat("1.21.4"), "My World/region/r.0.0.mca": "chunk"})
	f, err := Detect(base, "My World")
	if err != nil {
		t.Fatal(err)
	}
	if !f.WorldOnly || f.Version != "1.21.4" || f.Props["level-name"] != "world" {
		t.Errorf("got %+v", f)
	}
	if b, _ := os.ReadFile(filepath.Join(f.Root, "world", "region", "r.0.0.mca")); string(b) != "chunk" {
		t.Error("world not moved into the server folder")
	}
}

func TestDetectBedrockWorldOnly(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{"level.dat": "x", "levelname.txt": "Castle Build!", "db/CURRENT": ""})
	f, err := Detect(base, "upload")
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != store.TypeBedrock || f.Props["level-name"] != "Castle Build" {
		t.Errorf("got %+v", f)
	}
	if _, err := os.Stat(filepath.Join(f.Root, "worlds", "Castle Build", "db", "CURRENT")); err != nil {
		t.Error("bedrock world not placed under worlds/")
	}
}

func TestEmptyZipRejected(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{"readme.txt": "hi"})
	if _, err := Detect(base, "x"); err == nil {
		t.Error("random files accepted")
	}
}

func TestDetectWorldContainer(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{
		"server.properties":             "level-name=world\n",
		"bukkit.yml":                    "settings:\n  allow-end: true\n",
		"config/paper-global.yml":       "",
		"worlds/world/level.dat":        levelDat("26.3"),
		"worlds/world/dimensions/x.mca": "chunk",
		"worlds/world_nether/level.dat": levelDat("26.3"),
		"worlds/usercache.json":         "[]",
	})
	f, err := Detect(base, "x")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(base, "world", "dimensions", "x.mca")); string(b) != "chunk" {
		t.Fatal("world not moved out of the container")
	}
	if !exists(filepath.Join(base, "world_nether", "level.dat")) || !exists(filepath.Join(base, "usercache.json")) {
		t.Error("nether or usercache not moved")
	}
	if exists(filepath.Join(base, "worlds")) {
		t.Error("empty container left behind")
	}
	if len(f.Worlds) != 2 || len(f.Notes) == 0 {
		t.Errorf("got %+v", f)
	}
}

func TestBukkitWorldContainerLeftAlone(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{
		"server.properties":      "",
		"bukkit.yml":             "settings:\n  world-container: worlds\n",
		"worlds/world/level.dat": levelDat("26.3"),
	})
	if _, err := Detect(base, "x"); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(base, "worlds", "world", "level.dat")) {
		t.Error("world moved even though bukkit.yml points Paper at the container")
	}
}

func TestNoWorldWarns(t *testing.T) {
	base := t.TempDir()
	write(t, base, map[string]string{"server.properties": "", "config/paper-global.yml": ""})
	f, err := Detect(base, "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Notes) == 0 {
		t.Error("no warning that a new world will be generated")
	}
}
