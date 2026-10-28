// Package webmap finds a BlueMap or Dynmap web map that a server's plugin
// or mod is serving.
package webmap

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// Map is a web map served by the server.
type Map struct {
	Name string
	Port int
}

type source struct {
	name    string
	configs []string // where the config lives for plugins and for mods
	port    *regexp.Regexp
	def     int
}

var sources = []source{
	{"BlueMap", []string{"plugins/BlueMap/webserver.conf", "config/bluemap/webserver.conf"}, regexp.MustCompile(`^\s*port\s*[:=]\s*(\d+)`), 8100},
	{"Dynmap", []string{"plugins/dynmap/configuration.txt", "dynmap/configuration.txt"}, regexp.MustCompile(`^\s*webserver-port\s*:\s*(\d+)`), 8123},
}

var disabled = regexp.MustCompile(`^\s*enabled\s*[:=]\s*false`)

// Find returns the first web map set up in the server folder. The config
// files only appear once the plugin or mod has run, so a fresh install
// isn't found until the server has started with it.
func Find(dir string) (Map, bool) {
	for _, s := range sources {
		for _, c := range s.configs {
			f, err := os.Open(filepath.Join(dir, c))
			if err != nil {
				continue
			}
			port, off := s.def, false
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				l := sc.Text()
				if m := s.port.FindStringSubmatch(l); m != nil {
					if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n < 65536 {
						port = n
					}
				}
				// BlueMap's webserver.conf can turn its web server off.
				if s.name == "BlueMap" && disabled.MatchString(l) {
					off = true
				}
			}
			f.Close()
			if !off {
				return Map{s.name, port}, true
			}
		}
	}
	return Map{}, false
}
