// Package files lets the panel browse and change a server's folder. Every
// path is opened through os.Root, so nothing outside the folder can be
// reached, including through symlinks.
package files

import (
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// Entry is one item in a folder listing.
type Entry struct {
	Name     string
	Path     string // relative to the server folder, slash separated
	Dir      bool
	Size     int64
	Modified time.Time
}

// ErrBadName is returned for names that can't be used for a file.
var ErrBadName = errors.New("that name can't be used")

// Clean turns a user-supplied path into a slash-separated path relative to
// the server folder. The folder itself is "".
// Rooting the path first means ".." can never climb above the folder.
func Clean(p string) string {
	return strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(p, "\\", "/")), "/")
}

// name checks a single file or folder name.
func name(n string) (string, error) {
	n = strings.TrimSpace(n)
	if n == "" || n == "." || n == ".." || strings.ContainsAny(n, "/\\\x00") {
		return "", ErrBadName
	}
	return n, nil
}

func open(dir string) (*os.Root, error) { return os.OpenRoot(dir) }

// rootPath maps the relative path to what os.Root expects.
func rootPath(rel string) string {
	if rel == "" {
		return "."
	}
	return rel
}

// List returns a folder's contents, folders first, then by name.
func List(dir, rel string) ([]Entry, error) {
	root, err := open(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(rootPath(rel))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	items, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(items))
	for _, it := range items {
		e := Entry{Name: it.Name(), Path: path.Join(rel, it.Name())}
		// Stat through the root so a symlink is followed only if it stays inside.
		info, err := root.Stat(e.Path)
		if err != nil {
			continue
		}
		e.Dir, e.Size, e.Modified = info.IsDir(), info.Size(), info.ModTime()
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// IsDir reports whether rel is an existing folder.
func IsDir(dir, rel string) bool {
	root, err := open(dir)
	if err != nil {
		return false
	}
	defer root.Close()
	info, err := root.Stat(rootPath(rel))
	return err == nil && info.IsDir()
}

// Open opens a regular file for reading. The caller closes it.
func Open(dir, rel string) (*os.File, os.FileInfo, error) {
	if rel == "" {
		return nil, nil, os.ErrNotExist
	}
	root, err := open(dir)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, os.ErrNotExist
	}
	return f, info, nil
}

// Save writes r to a file called filename inside the folder rel, replacing
// any file of that name. It writes to a temporary file first so a failed
// upload leaves nothing half-written.
func Save(dir, rel, filename string, r io.Reader) error {
	n, err := name(path.Base(strings.ReplaceAll(filename, "\\", "/")))
	if err != nil {
		return err
	}
	root, err := open(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	target := path.Join(rel, n)
	if info, err := root.Lstat(target); err == nil && !info.Mode().IsRegular() {
		return errors.New(n + " already exists and isn't a file")
	}
	tmp := path.Join(rel, ".upload-"+n+".part")
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = root.Rename(tmp, target)
	}
	if err != nil {
		root.Remove(tmp)
	}
	return err
}

// Mkdir makes a new folder called folder inside rel.
func Mkdir(dir, rel, folder string) error {
	n, err := name(folder)
	if err != nil {
		return err
	}
	root, err := open(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Mkdir(path.Join(rel, n), 0o755)
}

// Delete removes a file, or a folder and everything in it.
func Delete(dir, rel string) error {
	if rel == "" {
		return ErrBadName
	}
	root, err := open(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if _, err := root.Lstat(rel); err != nil {
		return err
	}
	return root.RemoveAll(rel)
}
