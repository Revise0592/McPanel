// Package importer unpacks an uploaded server (or world) zip and works out
// what kind of server it is.
package importer

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/archive"
	"github.com/Revise0592/McPanel/internal/props"
	"github.com/Revise0592/McPanel/internal/store"
)

// Found describes an unpacked import.
type Found struct {
	Root      string // folder holding the server files, ready to move
	Type      string
	Version   string // Minecraft version, "" if unknown
	Name      string
	Port      int
	WorldOnly bool
	Worlds    []string
	Addons    int
	Notes     []string
	// Props are server.properties values to apply after the move.
	Props map[string]string
}

// Extract unpacks src into dest, skipping macOS metadata.
func Extract(src, dest string) error {
	return archive.ExtractZip(src, dest, func(name string) bool {
		return strings.HasPrefix(name, "__MACOSX/") || strings.HasSuffix(name, "/.DS_Store") || name == ".DS_Store"
	})
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// findRoot descends through single wrapper folders, as made by zipping a
// folder rather than its contents.
func findRoot(dir string) string {
	for {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return dir
		}
		var dirs []string
		files := 0
		for _, e := range ents {
			if e.Name() == "__MACOSX" {
				continue
			}
			if e.IsDir() {
				dirs = append(dirs, e.Name())
			} else {
				files++
			}
		}
		if files > 0 || len(dirs) != 1 || looksLikeWorld(dir) {
			return dir
		}
		dir = filepath.Join(dir, dirs[0])
	}
}

func looksLikeWorld(dir string) bool { return exists(filepath.Join(dir, "level.dat")) }

// Detect inspects an extracted folder. base is the folder the zip was
// unpacked into; fallbackName is used when nothing better is found.
func Detect(base, fallbackName string) (Found, error) {
	root := findRoot(base)
	f := Found{Root: root, Props: map[string]string{}}

	// A bare world rather than a whole server.
	if looksLikeWorld(root) {
		return detectWorld(base, root, fallbackName)
	}

	switch {
	// Paper ships NeoForged tooling under libraries/net/neoforged, so look
	// for the loaders themselves.
	case isDir(filepath.Join(root, "libraries", "net", "minecraftforge", "forge")),
		isDir(filepath.Join(root, "libraries", "net", "neoforged", "neoforge")),
		isDir(filepath.Join(root, "libraries", "net", "neoforged", "forge")):
		return f, errors.New("this looks like a Forge or NeoForge server, which McPanel can't run yet")
	case exists(filepath.Join(root, "bedrock_server")) || exists(filepath.Join(root, "bedrock_server.exe")) || bedrockWorlds(root) != nil:
		f.Type = store.TypeBedrock
	case exists(filepath.Join(root, "fabric-server-launch.jar")) || exists(filepath.Join(root, "fabric-server-launcher.properties")) || isDir(filepath.Join(root, ".fabric")):
		f.Type = store.TypeFabric
	case exists(filepath.Join(root, "config", "paper-global.yml")) || exists(filepath.Join(root, "paper.yml")) ||
		exists(filepath.Join(root, "spigot.yml")) || exists(filepath.Join(root, "bukkit.yml")) || isDir(filepath.Join(root, "plugins")):
		f.Type = store.TypePaper
		if !exists(filepath.Join(root, "config", "paper-global.yml")) && !exists(filepath.Join(root, "paper.yml")) {
			f.Notes = append(f.Notes, "This looks like a Spigot or Bukkit server. It will run on Paper, which supports the same plugins.")
		}
	case exists(filepath.Join(root, "server.properties")) || javaWorlds(root) != nil:
		f.Type = store.TypeVanilla
	default:
		return f, errors.New("couldn't find a Minecraft server or world in that zip")
	}

	p, _ := props.Load(filepath.Join(root, "server.properties"))
	if f.Type == store.TypeBedrock {
		f.Worlds = bedrockWorlds(root)
		f.Name = p.GetOr("server-name", "")
		fmt.Sscan(p.GetOr("server-port", "19132"), &f.Port)
		if exists(filepath.Join(root, "bedrock_server.exe")) && !exists(filepath.Join(root, "bedrock_server")) {
			f.Notes = append(f.Notes, "This is a Windows Bedrock server. McPanel will download the Linux version; worlds and settings are kept.")
		}
	} else {
		if moved := flattenWorldContainer(root); moved != "" {
			f.Notes = append(f.Notes, fmt.Sprintf("Worlds were stored in a %q folder; they've been moved next to the server so they load normally.", moved))
		}
		f.Worlds = javaWorlds(root)
		if len(f.Worlds) == 0 {
			f.Notes = append(f.Notes, "No world was found in this zip, so a new one will be generated on first start.")
		}
		f.Name = p.GetOr("motd", "")
		fmt.Sscan(p.GetOr("server-port", "25565"), &f.Port)
		f.Version = javaVersion(root)
		f.Addons = countJars(filepath.Join(root, "plugins")) + countJars(filepath.Join(root, "mods"))
	}
	if f.Name == "" || strings.Contains(f.Name, "§") || strings.Contains(f.Name, `\u`) || f.Name == "A Minecraft Server" || f.Name == "Dedicated Server" {
		f.Name = fallbackName
	}
	return f, nil
}

func detectWorld(base, root, fallbackName string) (Found, error) {
	f := Found{WorldOnly: true, Name: fallbackName, Props: map[string]string{}}
	name := fallbackName
	if b, err := os.ReadFile(filepath.Join(root, "levelname.txt")); err == nil && strings.TrimSpace(string(b)) != "" {
		name = strings.TrimSpace(string(b))
	}
	// Wrap the world in a fresh server folder.
	server := filepath.Join(base, ".mcpanel-server")
	if err := os.MkdirAll(server, 0o755); err != nil {
		return f, err
	}
	var dst string
	if isDir(filepath.Join(root, "db")) {
		f.Type = store.TypeBedrock
		f.Port = 19132
		folder := safeFolder(name)
		dst = filepath.Join(server, "worlds", folder)
		f.Props["level-name"] = folder
		f.Worlds = []string{folder}
	} else {
		f.Type = store.TypePaper
		f.Port = 25565
		dst = filepath.Join(server, "world")
		f.Props["level-name"] = "world"
		f.Worlds = []string{"world"}
		f.Notes = append(f.Notes, "This is a single world. Pick the server type and the version it was last played on.")
	}
	// Move the world's contents rather than the folder itself: when the
	// world sits at the top of the zip, root is base.
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return f, err
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return f, err
	}
	for _, e := range ents {
		if e.Name() == ".mcpanel-server" || e.Name() == "__MACOSX" {
			continue
		}
		if err := os.Rename(filepath.Join(root, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return f, err
		}
	}
	f.Root = server
	if f.Type != store.TypeBedrock {
		f.Version = worldVersion(dst)
	}
	return f, nil
}

var unsafeFolder = regexp.MustCompile(`[^A-Za-z0-9 _-]+`)

func safeFolder(s string) string {
	s = strings.TrimSpace(unsafeFolder.ReplaceAllString(s, ""))
	if s == "" {
		return "world"
	}
	return s
}

// skipContainers are folders that never hold worlds.
var skipContainers = map[string]bool{
	"plugins": true, "mods": true, "config": true, "libraries": true, "cache": true,
	"logs": true, "versions": true, "crash-reports": true, ".fabric": true, ".paper": true, "bundler": true,
}

// flattenWorldContainer handles servers that kept their worlds in a
// subfolder (Paper's --world-container / -W start option, common on
// hosting panels). McPanel starts servers without that option, so the
// worlds are moved up next to the server. It returns the container name,
// or "" if nothing was moved. Servers that set world-container in
// bukkit.yml are left alone, since Paper reads that itself.
func flattenWorldContainer(root string) string {
	if b, err := os.ReadFile(filepath.Join(root, "bukkit.yml")); err == nil && bukkitContainer.Match(b) {
		return ""
	}
	if len(javaWorlds(root)) > 0 {
		return ""
	}
	ents, _ := os.ReadDir(root)
	for _, e := range ents {
		if !e.IsDir() || skipContainers[e.Name()] {
			continue
		}
		container := filepath.Join(root, e.Name())
		worlds := javaWorlds(container)
		if len(worlds) == 0 {
			continue
		}
		for _, w := range worlds {
			os.Rename(filepath.Join(container, w), filepath.Join(root, w))
		}
		// Player name cache lives with the worlds when a container is used.
		if exists(filepath.Join(container, "usercache.json")) && !exists(filepath.Join(root, "usercache.json")) {
			os.Rename(filepath.Join(container, "usercache.json"), filepath.Join(root, "usercache.json"))
		}
		os.Remove(container) // only succeeds if now empty
		return e.Name()
	}
	return ""
}

var bukkitContainer = regexp.MustCompile(`(?m)^\s*world-container:\s*\S`)

func javaWorlds(root string) []string {
	ents, _ := os.ReadDir(root)
	var out []string
	for _, e := range ents {
		if e.IsDir() && looksLikeWorld(filepath.Join(root, e.Name())) {
			out = append(out, e.Name())
		}
	}
	return out
}

func bedrockWorlds(root string) []string {
	ents, _ := os.ReadDir(filepath.Join(root, "worlds"))
	var out []string
	for _, e := range ents {
		if e.IsDir() && isDir(filepath.Join(root, "worlds", e.Name(), "db")) {
			out = append(out, e.Name())
		}
	}
	return out
}

func countJars(dir string) int {
	ents, _ := os.ReadDir(dir)
	n := 0
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jar") {
			n++
		}
	}
	return n
}

var (
	mcInHistory  = regexp.MustCompile(`MC: ([0-9]+\.[0-9.]+)`)
	leadVersion  = regexp.MustCompile(`^([0-9]+\.[0-9]+(?:\.[0-9]+)?)-`)
	logVersion   = regexp.MustCompile(`Starting minecraft server version ([0-9]+\.[0-9]+(?:\.[0-9]+)?)\b`)
	plainVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?$`)
)

// javaVersion tries the places servers record their Minecraft version.
func javaVersion(root string) string {
	if b, err := os.ReadFile(filepath.Join(root, "version_history.json")); err == nil {
		var h struct {
			CurrentVersion string `json:"currentVersion"`
		}
		if json.Unmarshal(b, &h) == nil {
			if m := mcInHistory.FindStringSubmatch(h.CurrentVersion); m != nil {
				return m[1]
			}
			if m := leadVersion.FindStringSubmatch(h.CurrentVersion); m != nil {
				return m[1]
			}
		}
	}
	if b, err := readHead(filepath.Join(root, "logs", "latest.log"), 64<<10); err == nil {
		if m := logVersion.FindSubmatch(b); m != nil {
			return string(m[1])
		}
	}
	ents, _ := os.ReadDir(root)
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jar") {
			if v := jarVersion(filepath.Join(root, e.Name())); v != "" {
				return v
			}
		}
	}
	for _, w := range javaWorlds(root) {
		if v := worldVersion(filepath.Join(root, w)); v != "" {
			return v
		}
	}
	return ""
}

func readHead(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, n))
}

// jarVersion reads version.json from a Mojang server jar.
func jarVersion(path string) string {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "version.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return ""
		}
		defer rc.Close()
		var v struct {
			ID string `json:"id"`
		}
		if json.NewDecoder(io.LimitReader(rc, 1<<20)).Decode(&v) == nil && plainVersion.MatchString(v.ID) {
			return v.ID
		}
	}
	return ""
}

// worldVersion finds the version a Java world was last saved with by
// looking for the version name stored in level.dat. level.dat is gzipped
// NBT; rather than parse it fully, search for the "Name" tag inside the
// "Version" compound.
func worldVersion(dir string) string {
	b, err := gunzipFile(filepath.Join(dir, "level.dat"))
	if err != nil {
		return ""
	}
	// TAG_Compound "Version" ... TAG_String "Name" <len> <value>
	i := strings.Index(string(b), "\x0a\x00\x07Version")
	if i < 0 {
		return ""
	}
	rest := b[i:]
	j := strings.Index(string(rest), "\x08\x00\x04Name")
	if j < 0 || j+9 > len(rest) {
		return ""
	}
	p := rest[j+7:]
	n := int(p[0])<<8 | int(p[1])
	if n <= 0 || 2+n > len(p) {
		return ""
	}
	v := string(p[2 : 2+n])
	if plainVersion.MatchString(v) {
		return v
	}
	return ""
}

// CleanStale removes abandoned import folders older than a day.
func CleanStale(tmpDir string) {
	ents, _ := os.ReadDir(tmpDir)
	for _, e := range ents {
		if !strings.HasPrefix(e.Name(), "import-") {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
			os.RemoveAll(filepath.Join(tmpDir, e.Name()))
		}
	}
}
