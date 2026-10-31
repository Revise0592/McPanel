package velocity

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Revise0592/McPanel/internal/props"
)

// PaperConfig is where Paper (and its forks) keep proxy settings. Paper
// writes it on first start.
const PaperConfig = "config/paper-global.yml"

// ErrNotStarted means a server hasn't made its settings files yet.
var ErrNotStarted = errors.New("start it once first, so it creates its settings files")

func indent(l string) int { return len(l) - len(strings.TrimLeft(l, " ")) }

func yamlKey(l string) string {
	k, _, ok := strings.Cut(strings.TrimSpace(l), ":")
	if !ok || strings.HasPrefix(strings.TrimSpace(l), "#") {
		return ""
	}
	return k
}

// block finds a mapping key among lines[from:to] at the given indent and
// returns its line and the end of its children.
func block(lines []string, from, to, ind int, key string) (at, end int) {
	for i := from; i < to; i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			continue
		}
		if indent(l) == ind && yamlKey(l) == key {
			end = i + 1
			for end < to && (strings.TrimSpace(lines[end]) == "" || indent(lines[end]) > ind) {
				end++
			}
			// Don't count trailing blank lines as part of the block.
			for end > i+1 && strings.TrimSpace(lines[end-1]) == "" {
				end--
			}
			return i, end
		}
	}
	return -1, -1
}

// setYAML sets proxies.velocity.<key> for each key in values, adding
// whatever's missing.
func setYAML(lines []string, values [][2]string) []string {
	p, pEnd := block(lines, 0, len(lines), 0, "proxies")
	if p < 0 {
		lines = append(lines, "proxies:")
		p, pEnd = len(lines)-1, len(lines)
	}
	child := 2
	for i := p + 1; i < pEnd; i++ {
		if strings.TrimSpace(lines[i]) != "" {
			child = indent(lines[i])
			break
		}
	}
	v, vEnd := block(lines, p+1, pEnd, child, "velocity")
	if v < 0 {
		lines = append(lines[:pEnd], append([]string{strings.Repeat(" ", child) + "velocity:"}, lines[pEnd:]...)...)
		v, vEnd = pEnd, pEnd+1
	}
	grand := child * 2
	for i := v + 1; i < vEnd; i++ {
		if strings.TrimSpace(lines[i]) != "" {
			grand = indent(lines[i])
			break
		}
	}
	for _, kv := range values {
		line := strings.Repeat(" ", grand) + kv[0] + ": " + kv[1]
		found := false
		for i := v + 1; i < vEnd; i++ {
			if indent(lines[i]) == grand && yamlKey(lines[i]) == kv[0] {
				lines[i] = line
				found = true
				break
			}
		}
		if !found {
			lines = append(lines[:vEnd], append([]string{line}, lines[vEnd:]...)...)
			vEnd++
		}
	}
	return lines
}

func editPaper(dir string, values [][2]string) error {
	p := filepath.Join(dir, PaperConfig)
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotStarted
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	lines = setYAML(lines, values)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func setOnlineMode(dir string, on bool) error {
	f, err := props.Load(filepath.Join(dir, "server.properties"))
	if err != nil {
		return err
	}
	f.Set("online-mode", strconv.FormatBool(on))
	return f.Save()
}

// LinkPaper lets a Paper server take players forwarded by a proxy that
// shares secret. Players then sign in through the proxy, so the server's
// own account check is turned off; Paper refuses anyone who connects
// without the secret.
func LinkPaper(dir, secret string) error {
	err := editPaper(dir, [][2]string{
		{"enabled", "true"},
		{"online-mode", "true"},
		{"secret", "'" + strings.ReplaceAll(secret, "'", "''") + "'"},
	})
	if err != nil {
		return err
	}
	return setOnlineMode(dir, false)
}

// UnlinkPaper undoes LinkPaper, so players can join the server directly.
func UnlinkPaper(dir string) error {
	err := editPaper(dir, [][2]string{{"enabled", "false"}})
	if err != nil && !errors.Is(err, ErrNotStarted) {
		return err
	}
	return setOnlineMode(dir, true)
}

// PaperLinked reports whether a Paper server takes players from a proxy.
func PaperLinked(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, PaperConfig))
	if err != nil {
		return false
	}
	lines := strings.Split(string(b), "\n")
	p, pEnd := block(lines, 0, len(lines), 0, "proxies")
	if p < 0 {
		return false
	}
	for i := p + 1; i < pEnd; i++ {
		if yamlKey(lines[i]) != "velocity" {
			continue
		}
		_, vEnd := block(lines, i, pEnd, indent(lines[i]), "velocity")
		for j := i + 1; j < vEnd; j++ {
			if yamlKey(lines[j]) == "enabled" {
				_, v, _ := strings.Cut(lines[j], ":")
				return strings.TrimSpace(v) == "true"
			}
		}
	}
	return false
}
