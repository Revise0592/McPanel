package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/addons"
	"github.com/Revise0592/McPanel/internal/diskuse"
	"github.com/Revise0592/McPanel/internal/geyser"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/modpack"
	"github.com/Revise0592/McPanel/internal/store"
	"github.com/Revise0592/McPanel/internal/velocity"
)

// TestRenderServerPages renders the pages that mix several kinds of data,
// for a running server, so a template that reaches for the wrong field
// fails here instead of as a "page error".
func TestRenderServerPages(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(st, mc.NewManager(dir, st), "", false)
	if err != nil {
		t.Fatal(err)
	}
	v := serverView{
		Server:   store.Server{ID: "s", Name: "Survival", Type: store.TypePaper, Version: "1.21.4", Installed: true, MemoryMB: 4096},
		Status:   mc.Status{State: mc.Running, StartedAt: time.Now().Add(-time.Hour), Players: []string{"Steve"}},
		Usage:    mc.Usage{CPU: 12, MemoryMB: 2048, TPS: 19.9},
		HasUsage: true,
		Address:  "localhost:25565",
	}
	events := []activity.Event{{Time: time.Now(), Kind: activity.Crash, Text: "Server exited", Repeats: 1}}
	usage := usageData{serverView: v, Range: chartRanges[0], Ranges: chartRanges, HasData: true,
		Charts: []usageChart{{Title: "CPU", Now: "12%", Line: "M0,0L1,1", Area: "M0,100Z", Ticks: ticks(20, fmtPct)}}}

	pages := map[string]any{
		"console":  consoleData{activityData: activityData{serverView: v, Events: events, More: true}, Performance: usage},
		"activity": activityData{serverView: v, Events: events},
		"settings": settingsData{serverView: v, Section: "general", Memory: []int{2048, 4096}, HostMB: 4096, OthersMB: 2048},
		"files":    filesData{serverView: v},
	}
	bv := v
	bv.BedrockPort, bv.BedrockAddress = 19132, "localhost port 19132"
	for _, st := range []geyser.Status{{}, {Geyser: addons.Addon{File: "Geyser-Spigot.jar", Title: "Geyser", Version: "2.12.1", Enabled: true}}} {
		w := httptest.NewRecorder()
		a.render(w, httptest.NewRequest("GET", "/", nil), "addons", "t", "servers", addonsData{serverView: bv, Geyser: &st})
		if w.Code != 200 || !strings.Contains(w.Body.String(), "Bedrock players") {
			t.Errorf("addons with Geyser %+v: status %d: %s", st, w.Code, w.Body)
		}
	}
	for _, d := range []diskData{{ID: "s"}, {ID: "s", HasFree: true, Free: 1 << 30,
		Report: diskuse.Report{Parts: []diskuse.Part{{Label: "World", Size: 100}}, Total: 100, Backups: 10, OldLogs: 5}}} {
		w := httptest.NewRecorder()
		a.partial(w, "disk-usage", d)
		if w.Code != 200 {
			t.Errorf("disk-usage: status %d: %s", w.Code, w.Body)
		}
	}
	for name, data := range pages {
		w := httptest.NewRecorder()
		a.render(w, httptest.NewRequest("GET", "/", nil), name, "t", "servers", data)
		if w.Code != 200 {
			t.Errorf("%s: status %d: %s", name, w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	a.partial(w, "usage-card", usage)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Performance") {
		t.Errorf("usage-card: status %d: %s", w.Code, w.Body)
	}
}

// TestRenderNewStates covers sleeping servers, the Velocity proxy's pages
// and the public status page.
func TestRenderNewStates(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(st, mc.NewManager(dir, st), "", false)
	if err != nil {
		t.Fatal(err)
	}
	asleep := serverView{
		Server:  store.Server{ID: "s", Name: "Survival", Type: store.TypePurpur, Version: "1.21.4", Build: "2400", Installed: true, SleepAfterMins: 15, StatusToken: strings.Repeat("ab", 16)},
		Status:  mc.Status{State: mc.Sleeping},
		Address: "localhost:25565",
	}
	proxy := serverView{
		Server:  store.Server{ID: "p", Name: "Network", Type: store.TypeVelocity, Version: "4.3.1", Build: "45", Installed: true, MemoryMB: 512},
		Status:  mc.Status{State: mc.Running, Players: []string{"Steve"}},
		Address: "localhost:25577",
	}
	linked := asleep
	linked.ViaProxy = "Network"
	neo := asleep
	neo.Type, neo.Build = store.TypeNeoForge, "21.1.77"

	cases := []struct {
		name, page string
		data       any
		want       string
	}{
		{"asleep console", "console", consoleData{activityData: activityData{serverView: asleep}, Performance: usageData{serverView: asleep, Range: chartRanges[0], Ranges: chartRanges}}, "Wake up"},
		{"linked console", "console", consoleData{activityData: activityData{serverView: linked}, Performance: usageData{serverView: linked, Range: chartRanges[0], Ranges: chartRanges}}, "Players join through"},
		{"proxy console", "console", consoleData{activityData: activityData{serverView: proxy}, Performance: usageData{serverView: proxy, Range: chartRanges[0], Ranges: chartRanges}}, "glist"},
		{"sleep setting", "settings", settingsData{serverView: asleep, Section: "general", Memory: []int{2048}, SleepChoices: mc.SleepChoices, StatusURL: "http://x/s/abc"}, "After 15 minutes"},
		{"neoforge version", "settings", settingsData{serverView: neo, Section: "version", Versions: []string{"1.21.4"}}, "with the newest loader"},
		{"proxy settings", "settings", settingsData{serverView: proxy, Section: "game", Proxy: velocity.Settings{Port: 25577, MOTD: "Hi", MaxPlayers: 100}}, "Save proxy settings"},
		{"proxy network", "settings", settingsData{serverView: proxy, Section: "network", Backends: []backendChoice{{ID: "s", Name: "Survival", Type: store.TypePaper, Port: 25565, Linked: true, Start: true, Ready: true}, {ID: "c", Name: "Creative", Type: store.TypeFolia, Port: 25566}}}, "Start it once first"},
		{"proxy advanced", "settings", settingsData{serverView: proxy, Section: "advanced", RawFile: "velocity.toml", Raw: "bind = \"0.0.0.0:25577\""}, "velocity.toml"},
		{"proxy players", "players", playersData{serverView: proxy, Proxy: true}, "Velocity doesn't keep these"},
		{"status page", "status", publicView{Token: "t", Name: "Survival", Type: store.TypePaper, Version: "1.21.4", State: mc.Sleeping, Label: "Asleep", Address: "x:25565", MaxPlayers: 20}, "asleep to save power"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		nav := "servers"
		if c.page == "status" {
			nav = ""
		}
		a.render(w, httptest.NewRequest("GET", "/", nil), c.page, "t", nav, c.data)
		if w.Code != 200 || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s: status %d, want %q in: %s", c.name, w.Code, c.want, w.Body)
		}
	}
	w := httptest.NewRecorder()
	a.partial(w, "server-card", asleep)
	if !strings.Contains(w.Body.String(), "Starts when someone joins") || !strings.Contains(w.Body.String(), "badge sleeping") {
		t.Errorf("sleeping card: %s", w.Body)
	}
}

func TestRenderModpackPages(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(st, mc.NewManager(dir, st), "", false)
	if err != nil {
		t.Fatal(err)
	}
	hits := []modpack.Hit{{Slug: "cobblemon-fabric", Title: "Cobblemon", Author: "x", Categories: []string{"fabric", "adventure"}, Versions: []string{"1.20.1", "1.21.1"}}}
	v := modpack.Version{ID: "abc", VersionNumber: "1.8.4", GameVersions: []string{"1.21.1"}, VersionType: "release", Type: store.TypeFabric}
	pack := serverView{Server: store.Server{ID: "s", Name: "Pack", Type: store.TypeNeoForge, Version: "1.21.1", Build: "21.1.77", Installed: true,
		Modpack: &store.Modpack{Name: "All the Mods 10", Version: "2.3"}}}
	cases := []struct {
		page string
		data any
		want string
	}{
		{"modpacks", modpacksData{Hits: hits, Memory: packMemory}, "Fabric · up to Minecraft 1.21.1"},
		{"modpack", modpackData{Project: modpack.Project{Slug: "cobblemon-fabric", Title: "Cobblemon"}, Versions: []modpack.Version{v}, Memory: packMemory, Default: 4096}, "1.8.4 · Minecraft 1.21.1 · Fabric"},
		{"modpack", modpackData{Project: modpack.Project{Slug: "x", Title: "X"}, Err: "no releases"}, "no releases"},
		{"settings", settingsData{serverView: pack, Section: "version"}, "set up from All the Mods 10 2.3"},
		{"addons", addonsData{serverView: pack}, "came with All the Mods 10"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		a.render(w, httptest.NewRequest("GET", "/", nil), c.page, "t", "servers", c.data)
		if w.Code != 200 || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s: status %d, want %q in: %s", c.page, w.Code, c.want, w.Body)
		}
	}
	w := httptest.NewRecorder()
	a.partial(w, "server-header", pack)
	if !strings.Contains(w.Body.String(), "All the Mods 10 2.3 · NeoForge") {
		t.Errorf("header: %s", w.Body)
	}
}
