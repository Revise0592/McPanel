package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	ok := map[string]string{
		"":                 "",
		"/":                "",
		"world/datapacks/": "world/datapacks",
		"/world//./x":      "world/x",
		`world\datapacks`:  "world/datapacks",
		"a/b/../c":         "a/c",
	}
	for in, want := range ok {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q; want %q", in, got, want)
		}
	}
	// Leading ".." collapses against the root rather than escaping it.
	if got := Clean("../../etc"); got != "etc" {
		t.Errorf("Clean(../../etc) = %q", got)
	}
}

func TestSaveListDelete(t *testing.T) {
	dir := t.TempDir()
	if err := Mkdir(dir, "", "world"); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, "world", `C:\Users\me\pack.zip`, strings.NewReader("zip")); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, "", "server.properties", strings.NewReader("a=b")); err != nil {
		t.Fatal(err)
	}
	list, err := List(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || !list[0].Dir || list[0].Name != "world" || list[1].Size != 3 {
		t.Fatalf("unexpected listing: %+v", list)
	}
	f, info, err := Open(dir, "world/pack.zip")
	if err != nil || info.Size() != 3 {
		t.Fatalf("open: %v", err)
	}
	f.Close()
	if _, _, err := Open(dir, "world"); err == nil {
		t.Fatal("opened a folder as a file")
	}
	if err := Delete(dir, "world"); err != nil {
		t.Fatal(err)
	}
	if IsDir(dir, "world") {
		t.Fatal("folder still there")
	}
	if err := Delete(dir, ""); err == nil {
		t.Fatal("deleted the server folder")
	}
}

func TestBadNames(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"", ".", "..", "a/b"} {
		if err := Mkdir(dir, "", n); err == nil {
			t.Errorf("Mkdir(%q) worked", n)
		}
	}
	if err := Save(dir, "", "..", strings.NewReader("x")); err == nil {
		t.Error("saved a file called ..")
	}
}

func TestSymlinkEscape(t *testing.T) {
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644)
	dir := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skip(err)
	}
	if _, _, err := Open(dir, "link/secret"); err == nil {
		t.Fatal("read a file outside the server folder")
	}
	if err := Save(dir, "link", "new", strings.NewReader("x")); err == nil {
		t.Fatal("wrote a file outside the server folder")
	}
	// Removing the link itself is fine and must leave the target alone.
	if err := Delete(dir, "link"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "secret")); err != nil {
		t.Fatal("target was removed")
	}
}
