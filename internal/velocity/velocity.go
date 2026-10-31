// Package velocity reads and changes the settings of Velocity proxies, and
// the settings on Paper servers that let a proxy forward players to them.
//
// velocity.toml is edited line by line rather than re-encoded, so the
// comments Velocity writes into it are kept.
package velocity

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ConfigFile is Velocity's settings file, in the proxy's folder.
const ConfigFile = "velocity.toml"

// SecretFile holds the key the proxy and its servers share.
const SecretFile = "forwarding.secret"

// DefaultPort is Velocity's own default.
const DefaultPort = 25565

var (
	headerRe = regexp.MustCompile(`^\s*\[[^\]]+\]\s*(#.*)?$`)
	keyRe    = regexp.MustCompile(`^\s*("[^"]*"|[A-Za-z0-9_.-]+)\s*=`)
	tagRe    = regexp.MustCompile(`<[^<>]*>`)
	nameRe   = regexp.MustCompile(`[^a-z0-9-]+`)
)

func path(dir string) string { return filepath.Join(dir, ConfigFile) }

func readLines(dir string) ([]string, error) {
	b, err := os.ReadFile(path(dir))
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), nil
}

func writeLines(dir string, lines []string) error {
	tmp := path(dir) + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path(dir))
}

// topLevel returns the index of a key before the first [section], or -1.
func topLevel(lines []string, key string) int {
	for i, l := range lines {
		if headerRe.MatchString(l) {
			return -1
		}
		if m := keyRe.FindStringSubmatch(l); m != nil && strings.Trim(m[1], `"`) == key {
			return i
		}
	}
	return -1
}

// stringValue reads a quoted value such as `bind = "0.0.0.0:25565"`.
func stringValue(line string) string {
	_, v, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	v = strings.TrimSpace(v)
	if s, err := strconv.Unquote(v); err == nil {
		return s
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	if s, err := strconv.Unquote(v); err == nil {
		return s
	}
	return strings.Trim(v, `"'`)
}

// get reads a top-level string setting.
func get(dir, key string) (string, bool) {
	lines, err := readLines(dir)
	if err != nil {
		return "", false
	}
	i := topLevel(lines, key)
	if i < 0 {
		return "", false
	}
	return stringValue(lines[i]), true
}

// setLocked changes a top-level setting to an already encoded TOML value,
// adding it before the first section when it's missing.
func set(lines []string, key, value string) []string {
	line := key + " = " + value
	if i := topLevel(lines, key); i >= 0 {
		lines[i] = line
		return lines
	}
	for i, l := range lines {
		if headerRe.MatchString(l) {
			return append(lines[:i], append([]string{line, ""}, lines[i:]...)...)
		}
	}
	return append(lines, line)
}

// Bind is the address the proxy listens on. host is "" for every address.
func Bind(dir string) (host string, port int) {
	v, ok := get(dir, "bind")
	if !ok {
		return "", DefaultPort
	}
	h, p, err := net.SplitHostPort(v)
	if err != nil {
		return "", DefaultPort
	}
	port, err = strconv.Atoi(p)
	if err != nil {
		return "", DefaultPort
	}
	if h == "0.0.0.0" || h == "::" {
		h = ""
	}
	return h, port
}

// Port is the port players join on, or 0 if there's no settings file yet.
func Port(dir string) int {
	if _, err := os.Stat(path(dir)); err != nil {
		return 0
	}
	_, p := Bind(dir)
	return p
}

// MOTD is the proxy's description with its formatting tags removed.
func MOTD(dir string) string {
	v, _ := get(dir, "motd")
	return strings.TrimSpace(tagRe.ReplaceAllString(v, ""))
}

// Settings are the values McPanel manages in velocity.toml.
type Settings struct {
	Port       int
	MOTD       string
	MaxPlayers int
}

// Read returns the current settings.
func Read(dir string) Settings {
	s := Settings{Port: Port(dir), MOTD: MOTD(dir), MaxPlayers: 500}
	if lines, err := readLines(dir); err == nil {
		if i := topLevel(lines, "show-max-players"); i >= 0 {
			_, v, _ := strings.Cut(lines[i], "=")
			if n, err := strconv.Atoi(strings.Fields(v + " x")[0]); err == nil {
				s.MaxPlayers = n
			}
		}
	}
	return s
}

// Save writes the settings, keeping the listen address's host.
func Save(dir string, s Settings) error {
	lines, err := readLines(dir)
	if err != nil {
		return err
	}
	host, _ := Bind(dir)
	if host == "" {
		host = "0.0.0.0"
	}
	lines = set(lines, "bind", strconv.Quote(net.JoinHostPort(host, strconv.Itoa(s.Port))))
	lines = set(lines, "motd", strconv.Quote(s.MOTD))
	lines = set(lines, "show-max-players", strconv.Itoa(s.MaxPlayers))
	return writeLines(dir, lines)
}

// Prepare writes a first velocity.toml from the defaults inside Velocity's
// jar, if there isn't one yet: listening on port, described as motd, with
// no servers linked and a new forwarding secret.
func Prepare(dir, jar string, port int, motd string) error {
	if _, err := os.Stat(path(dir)); err == nil {
		return nil
	}
	b, err := defaultConfig(jar)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path(dir), b, 0o644); err != nil {
		return err
	}
	if _, err := Secret(dir); err != nil {
		return err
	}
	lines, err := readLines(dir)
	if err != nil {
		return err
	}
	lines = set(lines, "bind", strconv.Quote(net.JoinHostPort("0.0.0.0", strconv.Itoa(port))))
	lines = set(lines, "motd", strconv.Quote(motd))
	lines = set(lines, "player-info-forwarding-mode", `"modern"`)
	lines = replaceSection(lines, "servers", []string{"try = []"})
	lines = replaceSection(lines, "forced-hosts", nil)
	return writeLines(dir, lines)
}

func defaultConfig(jar string) ([]byte, error) {
	z, err := zip.OpenReader(jar)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name == "default-velocity.toml" {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(io.LimitReader(r, 1<<20))
		}
	}
	return nil, errors.New("Velocity's default settings weren't found in its jar")
}

// replaceSection swaps the settings in a [section] for body, keeping the
// comments at its top. A missing section is added at the end.
func replaceSection(lines []string, name string, body []string) []string {
	start := -1
	for i, l := range lines {
		if headerRe.MatchString(l) && strings.Trim(strings.TrimSpace(strings.SplitN(l, "#", 2)[0]), "[]") == name {
			start = i
			break
		}
	}
	if start < 0 {
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		out := append(lines, "", "["+name+"]")
		return append(append(out, body...), "")
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if headerRe.MatchString(lines[i]) {
			end = i
			break
		}
	}
	keep := start + 1
	for keep < end && strings.HasPrefix(strings.TrimSpace(lines[keep]), "#") {
		keep++
	}
	out := append([]string{}, lines[:keep]...)
	out = append(out, body...)
	if end < len(lines) {
		out = append(out, "")
	}
	return append(out, lines[end:]...)
}

// Secret returns the forwarding secret, creating one if there is none.
func Secret(dir string) (string, error) {
	p := filepath.Join(dir, SecretFile)
	if b, err := os.ReadFile(p); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s, nil
		}
	}
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := hex.EncodeToString(b)
	return s, os.WriteFile(p, []byte(s), 0o600)
}

// Backend is a server the proxy sends players to.
type Backend struct {
	Name string // as players see it in /server
	Port int
}

// ServerName turns a server's name in McPanel into one Velocity accepts.
func ServerName(name string) string {
	s := strings.Trim(nameRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if s == "" {
		s = "server"
	}
	return s
}

// Link lists the backends in velocity.toml, in the order players are tried,
// and turns on modern forwarding. Forced hosts are cleared, since Velocity
// won't start if they name a server that isn't listed.
func Link(dir string, backends []Backend) error {
	lines, err := readLines(dir)
	if err != nil {
		return err
	}
	if _, err := Secret(dir); err != nil {
		return err
	}
	var body, try []string
	seen := map[string]bool{}
	for _, b := range backends {
		name := b.Name
		for n := 2; seen[name]; n++ {
			name = fmt.Sprintf("%s-%d", b.Name, n)
		}
		seen[name] = true
		body = append(body, fmt.Sprintf("%s = %q", name, net.JoinHostPort("127.0.0.1", strconv.Itoa(b.Port))))
		try = append(try, strconv.Quote(name))
	}
	body = append(body, "try = ["+strings.Join(try, ", ")+"]")
	lines = set(lines, "player-info-forwarding-mode", `"modern"`)
	lines = set(lines, "forwarding-secret-file", strconv.Quote(SecretFile))
	lines = replaceSection(lines, "servers", body)
	lines = replaceSection(lines, "forced-hosts", nil)
	return writeLines(dir, lines)
}
