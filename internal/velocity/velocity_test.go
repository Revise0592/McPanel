package velocity

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// defaults is a cut-down copy of Velocity's default-velocity.toml.
const defaults = `# Config version. Do not change this
config-version = "2.9"

# What port should the proxy be bound to? By default, we'll bind to all addresses on port 25565.
bind = "0.0.0.0:25565"

# What should be the MOTD? This gets displayed when the player adds your server to
# their server list. Only MiniMessage format is accepted.
motd = "<#09add3>A Velocity Server"

show-max-players = 500

online-mode = true

player-info-forwarding-mode = "NONE"

forwarding-secret-file = "forwarding.secret"

[servers]
# Configure your servers here. Each key represents the server's name, and the value
# represents the IP address of the server to connect to.
lobby = "127.0.0.1:30066"
factions = "127.0.0.1:30067"

# In what order we should try servers when a player logs in or is kicked from a server.
try = [
    "lobby"
]

[forced-hosts]
# Configure your forced hosts here.
"lobby.example.com" = [
    "lobby"
]

[advanced]
compression-threshold = 256
`

func fakeJar(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "velocity.jar")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("default-velocity.toml")
	w.Write([]byte(defaults))
	z.Close()
	f.Close()
	return p
}

func TestPrepareAndLink(t *testing.T) {
	dir := t.TempDir()
	if err := Prepare(dir, fakeJar(t), 25570, "My network"); err != nil {
		t.Fatal(err)
	}
	if host, port := Bind(dir); host != "" || port != 25570 {
		t.Fatalf("bind %q %d", host, port)
	}
	if m := MOTD(dir); m != "My network" {
		t.Fatalf("motd %q", m)
	}
	secret, err := Secret(dir)
	if err != nil || len(secret) != 24 {
		t.Fatalf("secret %q %v", secret, err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ConfigFile))
	s := string(b)
	for _, gone := range []string{"lobby", "factions"} {
		if strings.Contains(s, gone) {
			t.Fatalf("default server %s is still listed:\n%s", gone, s)
		}
	}
	if !strings.Contains(s, `player-info-forwarding-mode = "modern"`) || !strings.Contains(s, "compression-threshold = 256") {
		t.Fatalf("settings lost:\n%s", s)
	}

	if err := Link(dir, []Backend{{"survival", 25565}, {"creative", 25566}, {"survival", 25567}}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, ConfigFile))
	s = string(b)
	for _, want := range []string{
		`survival = "127.0.0.1:25565"`,
		`creative = "127.0.0.1:25566"`,
		`survival-2 = "127.0.0.1:25567"`,
		`try = ["survival", "creative", "survival-2"]`,
		"# Configure your servers here.",
		"[forced-hosts]\n# Configure your forced hosts here.\n\n[advanced]",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Count(s, "[servers]") != 1 || strings.Count(s, "try =") != 1 {
		t.Fatalf("sections duplicated:\n%s", s)
	}

	if err := Save(dir, Settings{Port: 25580, MOTD: `Say "hi"`, MaxPlayers: 40}); err != nil {
		t.Fatal(err)
	}
	if got := Read(dir); got.Port != 25580 || got.MOTD != `Say "hi"` || got.MaxPlayers != 40 {
		t.Fatalf("read back %+v", got)
	}
	// Prepare leaves an existing file alone.
	if err := Prepare(dir, fakeJar(t), 1, "x"); err != nil || Port(dir) != 25580 {
		t.Fatalf("Prepare overwrote settings: %v", err)
	}
}

const paperGlobal = `_version: 29
messages:
  kick:
    connection-throttle: Connection throttled! Please wait before reconnecting.
proxies:
  bungee-cord:
    online-mode: true
  proxy-protocol: false
  velocity:
    enabled: false
    online-mode: true
    secret: ''
scoreboards:
  save-empty-scoreboard-teams: true
`

func TestLinkPaper(t *testing.T) {
	dir := t.TempDir()
	if err := LinkPaper(dir, "abc"); err != ErrNotStarted {
		t.Fatalf("want ErrNotStarted, got %v", err)
	}
	os.MkdirAll(filepath.Join(dir, "config"), 0o755)
	os.WriteFile(filepath.Join(dir, PaperConfig), []byte(paperGlobal), 0o644)
	os.WriteFile(filepath.Join(dir, "server.properties"), []byte("online-mode=true\nmotd=Hi\n"), 0o644)
	if err := LinkPaper(dir, "it's"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, PaperConfig))
	want := strings.Replace(strings.Replace(paperGlobal, "enabled: false", "enabled: true", 1), "secret: ''", "secret: 'it''s'", 1)
	if string(b) != want {
		t.Fatalf("got:\n%s", b)
	}
	if !PaperLinked(dir) {
		t.Fatal("not reported as linked")
	}
	p, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	if !strings.Contains(string(p), "online-mode=false") {
		t.Fatalf("online-mode not turned off: %s", p)
	}
	if err := UnlinkPaper(dir); err != nil {
		t.Fatal(err)
	}
	if PaperLinked(dir) {
		t.Fatal("still linked")
	}
	p, _ = os.ReadFile(filepath.Join(dir, "server.properties"))
	if !strings.Contains(string(p), "online-mode=true") {
		t.Fatalf("online-mode not turned back on: %s", p)
	}
}

func TestSetYAMLAddsMissing(t *testing.T) {
	lines := setYAML([]string{"_version: 29", "proxies:", "  proxy-protocol: false", "misc: {}"}, [][2]string{{"enabled", "true"}})
	got := strings.Join(lines, "\n")
	want := "_version: 29\nproxies:\n  proxy-protocol: false\n  velocity:\n    enabled: true\nmisc: {}"
	if got != want {
		t.Fatalf("got:\n%s", got)
	}
}
