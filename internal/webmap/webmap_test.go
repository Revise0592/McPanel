package webmap

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(body), 0o644)
}

func TestFind(t *testing.T) {
	if _, ok := Find(t.TempDir()); ok {
		t.Error("found a map in an empty folder")
	}

	dir := t.TempDir()
	write(t, dir, "plugins/BlueMap/webserver.conf", "# comment\nenabled: true\nport: 8200\n")
	if m, ok := Find(dir); !ok || m != (Map{"BlueMap", 8200}) {
		t.Errorf("BlueMap = %+v, %v", m, ok)
	}

	dir = t.TempDir()
	write(t, dir, "config/bluemap/webserver.conf", "enabled: false\nport: 8100\n")
	write(t, dir, "dynmap/configuration.txt", "deftemplatesuffix: hires\nwebserver-port: 8124\n")
	if m, ok := Find(dir); !ok || m != (Map{"Dynmap", 8124}) {
		t.Errorf("with BlueMap off, got %+v, %v", m, ok)
	}
}
