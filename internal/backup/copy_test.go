package backup

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCopyOut(t *testing.T) {
	data, out := t.TempDir(), t.TempDir()
	id, name := "survival-a1b2c3", "20261028-050000-auto.tar.gz"
	os.MkdirAll(Dir(data, id), 0o755)
	os.WriteFile(filepath.Join(Dir(data, id), name), []byte("backup"), 0o644)
	os.WriteFile(filepath.Join(Dir(data, id), infoFile), []byte(`{"name":"Survival"}`), 0o644)

	if err := copyOut(data, id, name, out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, id, name)); string(b) != "backup" {
		t.Errorf("copy holds %q", b)
	}
	if _, err := os.Stat(filepath.Join(out, id, infoFile)); err != nil {
		t.Errorf("info file not copied: %v", err)
	}
}

func TestCheckCopyDir(t *testing.T) {
	data, out := t.TempDir(), t.TempDir()
	if err := CheckCopyDir(data, out); err != nil {
		t.Errorf("good folder refused: %v", err)
	}
	inside := filepath.Join(data, "copies")
	os.Mkdir(inside, 0o755)
	for _, bad := range []string{"relative/path", filepath.Join(out, "missing"), inside, data} {
		if CheckCopyDir(data, bad) == nil {
			t.Errorf("CheckCopyDir(%q) accepted", bad)
		}
	}
}

func TestWorldFolders(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "server.properties"), []byte("level-name=smp\n"), 0o644)
	if got := WorldFolders("paper", dir); !slices.Equal(got, []string{"smp", "smp_nether", "smp_the_end"}) {
		t.Errorf("Java worlds = %v", got)
	}
	if got := WorldFolders("bedrock", dir); !slices.Equal(got, []string{"worlds"}) {
		t.Errorf("Bedrock worlds = %v", got)
	}
}
