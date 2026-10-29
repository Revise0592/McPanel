package diskuse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Revise0592/McPanel/internal/store"
)

func write(t *testing.T, path string, n int) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(strings.Repeat("x", n)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMeasureAndClear(t *testing.T) {
	dir, backups := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "world", "region", "r.0.0.mca"), 500)
	write(t, filepath.Join(dir, "world_nether", "level.dat"), 100)
	write(t, filepath.Join(dir, "plugins", "a.jar"), 50)
	write(t, filepath.Join(dir, "logs", "latest.log"), 20)
	write(t, filepath.Join(dir, "logs", "2026-10-01-1.log.gz"), 30)
	write(t, filepath.Join(dir, "crash-reports", "crash.txt"), 5)
	write(t, filepath.Join(dir, "paper.jar"), 200)
	write(t, filepath.Join(backups, "b.zip"), 70)

	r := Measure(store.TypePaper, dir, backups)
	got := map[string]int64{}
	for _, p := range r.Parts {
		got[p.Label] = p.Size
	}
	want := map[string]int64{"World": 600, "Plugins": 50, "Logs and crash reports": 55, "Server software and settings": 200}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d, want %d (all: %v)", k, got[k], v, got)
		}
	}
	if r.Parts[0].Label != "World" || r.Total != 905 || r.Backups != 70 || r.OldLogs != 35 {
		t.Errorf("report = %+v", r)
	}

	freed, err := ClearOldLogs(dir)
	if err != nil || freed != 35 {
		t.Fatalf("ClearOldLogs = %d, %v", freed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "logs", "latest.log")); err != nil {
		t.Error("latest.log should be kept")
	}
	if Measure(store.TypePaper, dir, backups).OldLogs != 0 {
		t.Error("old logs left behind")
	}
}
