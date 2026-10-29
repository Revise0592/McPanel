package geyser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindAndRemove(t *testing.T) {
	dir := t.TempDir()
	plugins := filepath.Join(dir, "plugins")
	os.MkdirAll(plugins, 0o755)
	for _, f := range []string{"Geyser-Spigot.jar", "floodgate-spigot.jar.disabled", "GeyserSkinManager.jar", "EssentialsX.jar"} {
		os.WriteFile(filepath.Join(plugins, f), []byte("x"), 0o644)
	}
	st := Find(dir)
	if st.Geyser.File != "Geyser-Spigot.jar" || st.Floodgate.File != "floodgate-spigot.jar.disabled" {
		t.Fatalf("Find = %+v", st)
	}
	if !st.On() {
		t.Error("Geyser is installed and on")
	}
	if err := Remove(dir); err != nil {
		t.Fatal(err)
	}
	left, _ := os.ReadDir(plugins)
	var names []string
	for _, e := range left {
		names = append(names, e.Name())
	}
	if len(names) != 2 || Find(dir).Geyser.File != "" {
		t.Errorf("after Remove: %v", names)
	}
}
