package backup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Revise0592/McPanel/internal/store"
)

func TestOrphans(t *testing.T) {
	data := t.TempDir()
	touch := func(id, name string) {
		dir := Dir(data, id)
		os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("12345"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	touch("alive-aaaaaa", "20260101-120000-manual.tar.gz")
	touch("old-world-bbbbbb", "20260101-120000-auto.tar.gz")
	touch("old-world-bbbbbb", "20260102-120000-manual.tar.gz")
	touch("named-cccccc", "20260101-120000.tar.gz")
	SaveInfo(data, store.Server{ID: "named-cccccc", Name: "My Server", Type: "paper", Version: "1.21"})
	touch("empty-dddddd", "notes.txt") // no backups, so not listed

	got, err := Orphans(data, func(id string) bool { return id == "alive-aaaaaa" })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d orphans, want 2: %+v", len(got), got)
	}
	// Largest first.
	if got[0].ID != "old-world-bbbbbb" || got[0].Name != "old world" || len(got[0].Backups) != 2 || got[0].Size != 10 {
		t.Errorf("first orphan = %+v", got[0])
	}
	if got[1].Name != "My Server" || got[1].Type != "paper" {
		t.Errorf("info file not used: %+v", got[1])
	}

	if err := DeleteFolder(data, "../escape"); err == nil {
		t.Error("DeleteFolder accepted a path outside the backups folder")
	}
	if err := DeleteFolder(data, "old-world-bbbbbb"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Dir(data, "old-world-bbbbbb")); !os.IsNotExist(err) {
		t.Error("folder still exists after DeleteFolder")
	}
}
