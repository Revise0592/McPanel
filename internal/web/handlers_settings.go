package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/backup"
	"github.com/Revise0592/McPanel/internal/icon"
	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/props"
	"github.com/Revise0592/McPanel/internal/store"
)

// field is one labelled control for a server.properties key.
type field struct {
	Key     string
	Label   string
	Help    string
	Kind    string // text, number, bool, select
	Options []string
	Default string
	Value   string
}

func (f field) Checked() bool { return f.Value == "true" }

var javaFields = []field{
	{Key: "motd", Label: "Server description", Help: "Shown under the server name in the multiplayer list.", Kind: "text", Default: "A Minecraft Server"},
	{Key: "gamemode", Label: "Game mode", Kind: "select", Options: []string{"survival", "creative", "adventure", "spectator"}, Default: "survival"},
	{Key: "difficulty", Label: "Difficulty", Kind: "select", Options: []string{"peaceful", "easy", "normal", "hard"}, Default: "easy"},
	{Key: "max-players", Label: "Max players", Kind: "number", Default: "20"},
	{Key: "pvp", Label: "Players can hurt each other (PvP)", Kind: "bool", Default: "true"},
	{Key: "hardcore", Label: "Hardcore", Help: "Players are banned when they die.", Kind: "bool", Default: "false"},
	{Key: "allow-flight", Label: "Allow flight", Help: "Needed by some mods and plugins; otherwise players get kicked for flying.", Kind: "bool", Default: "false"},
	{Key: "online-mode", Label: "Require Minecraft accounts", Help: "Turn off only for offline/LAN play. Anyone can join under any name when off.", Kind: "bool", Default: "true"},
	{Key: "spawn-protection", Label: "Spawn protection radius", Help: "Blocks around spawn that only operators can change. 0 turns it off.", Kind: "number", Default: "16"},
	{Key: "view-distance", Label: "View distance (chunks)", Kind: "number", Default: "10"},
	{Key: "simulation-distance", Label: "Simulation distance (chunks)", Kind: "number", Default: "10"},
	{Key: "level-name", Label: "World folder", Help: "Change to switch to (or create) a different world.", Kind: "text", Default: "world"},
	{Key: "level-seed", Label: "World seed", Help: "Only used when a new world is created.", Kind: "text"},
	{Key: "server-port", Label: "Port", Kind: "number", Default: "25565"},
}

var bedrockFields = []field{
	{Key: "server-name", Label: "Server name", Kind: "text", Default: "Dedicated Server"},
	{Key: "gamemode", Label: "Game mode", Kind: "select", Options: []string{"survival", "creative", "adventure"}, Default: "survival"},
	{Key: "difficulty", Label: "Difficulty", Kind: "select", Options: []string{"peaceful", "easy", "normal", "hard"}, Default: "easy"},
	{Key: "max-players", Label: "Max players", Kind: "number", Default: "10"},
	{Key: "allow-cheats", Label: "Allow cheats", Kind: "bool", Default: "false"},
	{Key: "online-mode", Label: "Require Xbox accounts", Kind: "bool", Default: "true"},
	{Key: "view-distance", Label: "View distance (chunks)", Kind: "number", Default: "32"},
	{Key: "tick-distance", Label: "Simulation distance (chunks)", Kind: "number", Default: "4"},
	{Key: "level-name", Label: "World name", Help: "Change to switch to (or create) a different world.", Kind: "text", Default: "Bedrock level"},
	{Key: "level-seed", Label: "World seed", Help: "Only used when a new world is created.", Kind: "text"},
	{Key: "server-port", Label: "Port (IPv4)", Kind: "number", Default: "19132"},
	{Key: "server-portv6", Label: "Port (IPv6)", Kind: "number", Default: "19133"},
}

func fieldsFor(typ string) []field {
	if typ == store.TypeBedrock {
		return bedrockFields
	}
	return javaFields
}

func propsPath(inst *mc.Instance) string { return filepath.Join(inst.Dir(), "server.properties") }

type settingsData struct {
	serverView
	Section  string // general, game, version or advanced
	Fields   []field
	Raw      string
	Versions []string
	Memory   []int
}

var settingsSections = []string{"general", "game", "version", "advanced"}

// settingsURL is the address of one section of a server's settings.
func settingsURL(id, section string) string {
	u := "/servers/" + id + "/settings"
	if section != "general" {
		u += "?s=" + section
	}
	return u
}

func (a *App) settingsPage(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	d := settingsData{serverView: a.view(r, sv), Section: r.URL.Query().Get("s")}
	if !slices.Contains(settingsSections, d.Section) {
		d.Section = "general"
	}
	switch d.Section {
	case "general":
		d.Memory = []int{1024, 2048, 3072, 4096, 6144, 8192, 12288, 16384}
		if !slices.Contains(d.Memory, sv.MemoryMB) && sv.MemoryMB > 0 {
			d.Memory = append(d.Memory, sv.MemoryMB)
			slices.Sort(d.Memory)
		}
	case "game", "advanced":
		f, err := props.Load(propsPath(inst))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		d.Fields = slices.Clone(fieldsFor(sv.Type))
		for i := range d.Fields {
			d.Fields[i].Value = f.GetOr(d.Fields[i].Key, d.Fields[i].Default)
		}
		d.Raw = f.Raw()
	case "version":
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		d.Versions, _ = install.Versions(ctx, sv.Type)
	}
	d.Tab = "settings"
	a.render(w, r, "settings", sv.Name+" settings", "servers", d)
}

func restartHint(inst *mc.Instance) string {
	if inst.Status().Online() {
		return " Restart the server to apply."
	}
	return ""
}

func (a *App) saveGameSettings(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	to := settingsURL(sv.ID, "game")
	f, err := props.Load(propsPath(inst))
	if err != nil {
		setError(w, err.Error())
		back(w, r, to)
		return
	}
	used := a.Mgr.UsedPorts(sv.ID)
	for _, fd := range fieldsFor(sv.Type) {
		val := strings.TrimSpace(r.FormValue(fd.Key))
		switch fd.Kind {
		case "bool":
			val = strconv.FormatBool(r.FormValue(fd.Key) == "on")
		case "number":
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 {
				setError(w, fd.Label+" must be a whole number.")
				back(w, r, to)
				return
			}
			if strings.HasPrefix(fd.Key, "server-port") {
				if n < 1 || n > 65535 {
					setError(w, "Ports must be between 1 and 65535.")
					back(w, r, to)
					return
				}
				if who := used[n]; who != "" {
					setError(w, fmt.Sprintf("Port %d is already used by %s.", n, who))
					back(w, r, to)
					return
				}
			}
		case "select":
			if !slices.Contains(fd.Options, val) {
				continue
			}
		}
		// Don't write keys the user never touched and that match defaults.
		if _, exists := f.Get(fd.Key); !exists && val == fd.Default {
			continue
		}
		f.Set(fd.Key, val)
	}
	if store.IsJava(sv.Type) {
		// Hardcore worlds need hard difficulty to behave as expected.
		if v, _ := f.Get("hardcore"); v == "true" {
			f.Set("difficulty", "hard")
		}
	}
	if err := f.Save(); err != nil {
		setError(w, err.Error())
	} else {
		setFlash(w, "Game settings saved."+restartHint(inst))
	}
	back(w, r, to)
}

func (a *App) saveServerSettings(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	to := settingsURL(sv.ID, "general")
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len(name) > 40 {
		setError(w, "The name must be 1 to 40 characters.")
		back(w, r, to)
		return
	}
	javaArgs := strings.TrimSpace(r.FormValue("javaArgs"))
	if strings.ContainsAny(javaArgs, "\n\r") {
		javaArgs = strings.Join(strings.Fields(javaArgs), " ")
	}
	_, err := a.Store.UpdateServer(sv.ID, func(s *store.Server) {
		s.Name = name
		s.AutoStart = r.FormValue("autoStart") == "on"
		s.RestartOnCrash = r.FormValue("restartOnCrash") == "on"
		if store.IsJava(s.Type) {
			s.MemoryMB = formInt(r, "memory", s.MemoryMB, 512, 65536)
			s.JavaArgs = javaArgs
		}
	})
	if err != nil {
		setError(w, err.Error())
	} else {
		setFlash(w, "Saved."+restartHint(inst))
	}
	back(w, r, to)
}

func (a *App) saveRawProperties(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	f, err := props.Load(propsPath(inst))
	if err == nil {
		f.SetRaw(r.FormValue("raw"))
		err = f.Save()
	}
	if err != nil {
		setError(w, err.Error())
	} else {
		setFlash(w, "server.properties saved."+restartHint(inst))
	}
	back(w, r, settingsURL(sv.ID, "advanced"))
}

func (a *App) changeVersion(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	version := r.FormValue("version")
	if version == "" {
		version = sv.Version
	}
	dataDir := a.Mgr.DataDir()
	go a.Mgr.Install(sv.ID, version, func(inst *mc.Instance) error {
		if !sv.Installed {
			return nil
		}
		inst.Logf("Backing up before changing version")
		_, err := backup.Create(dataDir, inst, "before-update")
		return err
	})
	setFlash(w, "Updating. A backup is made first; progress shows in the console.")
	back(w, r, "/servers/"+sv.ID)
}

const maxIconUpload = 10 << 20

func iconFile(inst *mc.Instance) string { return filepath.Join(inst.Dir(), icon.FileName) }

func (a *App) serverIcon(w http.ResponseWriter, r *http.Request) {
	_, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	setImageCache(w, r)
	http.ServeFile(w, r, iconFile(inst))
}

func (a *App) uploadServerIcon(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	to := settingsURL(sv.ID, "general")
	if !store.IsJava(sv.Type) {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxIconUpload)
	file, _, err := r.FormFile("icon")
	if err != nil {
		setError(w, "Choose a picture to upload (up to 10 MB).")
		back(w, r, to)
		return
	}
	defer file.Close()
	b, err := icon.Make(file)
	if err == nil {
		err = os.WriteFile(iconFile(inst), b, 0o644)
	}
	if err != nil {
		setError(w, "Couldn't set the icon: "+err.Error())
	} else {
		setFlash(w, "Server icon saved."+restartHint(inst))
	}
	back(w, r, to)
}

func (a *App) removeServerIcon(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	if err := os.Remove(iconFile(inst)); err != nil && !os.IsNotExist(err) {
		setError(w, "Couldn't remove the icon: "+err.Error())
	} else {
		setFlash(w, "Server icon removed."+restartHint(inst))
	}
	back(w, r, settingsURL(sv.ID, "general"))
}

const maxBedrockZip = 512 << 20

func (a *App) uploadBedrockZip(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	to := settingsURL(sv.ID, "version")
	if sv.Type != store.TypeBedrock {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBedrockZip)
	file, hdr, err := r.FormFile("zip")
	if err != nil {
		setError(w, "Choose the Bedrock server zip to upload (up to 512 MB).")
		back(w, r, to)
		return
	}
	defer file.Close()
	tmpDir := filepath.Join(a.Mgr.DataDir(), "tmp")
	os.MkdirAll(tmpDir, 0o755)
	out, err := os.CreateTemp(tmpDir, "bedrock-*.zip")
	if err == nil {
		_, err = io.Copy(out, file)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		if out != nil {
			os.Remove(out.Name())
		}
		setError(w, "Upload failed: "+err.Error())
		back(w, r, to)
		return
	}
	dataDir := a.Mgr.DataDir()
	go a.Mgr.InstallBedrockZip(sv.ID, out.Name(), install.BedrockZipVersion(hdr.Filename), func(inst *mc.Instance) error {
		if !sv.Installed {
			return nil
		}
		inst.Logf("Backing up before updating")
		_, err := backup.Create(dataDir, inst, "before-update")
		return err
	})
	setFlash(w, "Uploaded. Installing now; progress shows in the console.")
	back(w, r, "/servers/"+sv.ID)
}

func (a *App) deleteServer(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.load(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(r.FormValue("confirm")) != sv.Name {
		setError(w, "Type the server's name exactly to delete it.")
		back(w, r, settingsURL(sv.ID, "advanced"))
		return
	}
	backup.SaveInfo(a.Mgr.DataDir(), sv) // keeps the name for its leftover backups
	if err := a.Mgr.Remove(sv.ID); err != nil {
		setError(w, "Couldn't delete: "+err.Error())
		back(w, r, settingsURL(sv.ID, "advanced"))
		return
	}
	setFlash(w, sv.Name+" was deleted. Its backups are kept under Backups from deleted servers.")
	back(w, r, "/")
}
