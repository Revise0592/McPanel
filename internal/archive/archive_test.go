package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func writeZip(t *testing.T, path string, files map[string]string) {
	f, _ := os.Create(path)
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
}

func TestExtractZipRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "evil.zip")
	writeZip(t, src, map[string]string{"../escape.txt": "x"})
	if err := ExtractZip(src, filepath.Join(dir, "out"), nil); err == nil {
		t.Fatal("expected an error for a path outside the target")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Fatal("file escaped the target directory")
	}
}

func TestExtractZipSkip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.zip")
	writeZip(t, src, map[string]string{"keep.txt": "new", "server.properties": "new"})
	out := filepath.Join(dir, "out")
	os.MkdirAll(out, 0o755)
	os.WriteFile(filepath.Join(out, "server.properties"), []byte("mine"), 0o644)
	err := ExtractZip(src, out, func(n string) bool { return n == "server.properties" })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "server.properties")); string(b) != "mine" {
		t.Errorf("skipped file was overwritten: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "keep.txt")); string(b) != "new" {
		t.Errorf("keep.txt = %q", b)
	}
}

func TestExtractTarGzRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "evil.tar.gz")
	f, _ := os.Create(src)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../../etc"})
	tw.Close()
	gz.Close()
	f.Close()
	if err := ExtractTarGz(src, filepath.Join(dir, "out"), 0, nil); err == nil {
		t.Fatal("expected an error for a symlink leaving the target")
	}
}

func TestTarGzRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "srv")
	os.MkdirAll(filepath.Join(src, "world", "region"), 0o755)
	os.MkdirAll(filepath.Join(src, "plugins", ".paper-remapped"), 0o755)
	os.WriteFile(filepath.Join(src, "world", "region", "r.0.0.mca"), []byte("chunk"), 0o644)
	os.WriteFile(filepath.Join(src, "plugins", ".paper-remapped", "big.jar"), []byte("cache"), 0o644)
	os.WriteFile(filepath.Join(src, "plugins", "a.jar"), []byte("plugin"), 0o644)

	dst := filepath.Join(dir, "b.tar.gz")
	skip := func(rel string) bool { return rel == "plugins/.paper-remapped" }
	if err := CreateTarGz(dst, src, []string{"world", "plugins"}, skip, nil); err != nil {
		t.Fatal(err)
	}
	tops, err := TopLevel(dst)
	if err != nil || len(tops) != 2 {
		t.Fatalf("TopLevel = %v, %v", tops, err)
	}
	out := filepath.Join(dir, "out")
	if err := ExtractTarGz(dst, out, 0, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "world", "region", "r.0.0.mca")); string(b) != "chunk" {
		t.Errorf("world file = %q", b)
	}
	if _, err := os.Stat(filepath.Join(out, "plugins", ".paper-remapped")); err == nil {
		t.Error("skipped folder was archived")
	}
	if _, err := os.Stat(filepath.Join(out, "plugins", "a.jar")); err != nil {
		t.Error("plugin jar missing")
	}
}
