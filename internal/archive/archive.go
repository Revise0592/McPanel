// Package archive extracts and creates zip and tar.gz archives safely.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// safeJoin joins name onto dir, refusing paths that escape dir.
func safeJoin(dir, name string) (string, error) {
	p := filepath.Join(dir, name)
	if p != filepath.Clean(dir) && !strings.HasPrefix(p, filepath.Clean(dir)+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe path in archive: %q", name)
	}
	return p, nil
}

func stripComponents(name string, n int) string {
	name = strings.TrimPrefix(filepath.ToSlash(name), "./")
	for i := 0; i < n; i++ {
		_, rest, ok := strings.Cut(name, "/")
		if !ok {
			return ""
		}
		name = rest
	}
	return name
}

// SkipFunc decides whether an archive entry (slash-separated, relative)
// should be left alone instead of being written.
type SkipFunc func(name string) bool

// ProgressFunc is told how many bytes of total have been handled so far.
type ProgressFunc func(done, total int64)

// counter counts bytes passing through and reports them to fn.
type counter struct {
	r     io.Reader
	done  int64
	total int64
	fn    ProgressFunc
}

func (c *counter) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.add(int64(n))
	return n, err
}

func (c *counter) add(n int64) {
	c.done += n
	if c.fn != nil {
		c.fn(c.done, c.total)
	}
}

// ExtractZip extracts src into dir. Entries for which skip returns true
// are not written.
func ExtractZip(src, dir string, skip SkipFunc) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := strings.TrimPrefix(filepath.ToSlash(f.Name), "./")
		if name == "" || (skip != nil && skip(name)) {
			continue
		}
		dst, err := safeJoin(dir, name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		mode := f.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		err = writeFile(dst, rc, mode|0o200)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// ExtractTarGz extracts src into dir, dropping the first strip path components.
// prog, if set, follows how much of src has been read.
func ExtractTarGz(src, dir string, strip int, prog ProgressFunc) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	var size int64
	if info, err := f.Stat(); err == nil {
		size = info.Size()
	}
	gz, err := gzip.NewReader(&counter{r: f, total: size, fn: prog})
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := stripComponents(h.Name, strip)
		if name == "" {
			continue
		}
		dst, err := safeJoin(dir, name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := writeFile(dst, tr, os.FileMode(h.Mode).Perm()|0o200); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Only allow relative links that stay inside dir.
			target := filepath.Join(filepath.Dir(dst), h.Linkname)
			if filepath.IsAbs(h.Linkname) {
				return fmt.Errorf("unsafe symlink in archive: %q", h.Name)
			}
			if _, err := safeJoin(dir, mustRel(dir, target)); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			os.Remove(dst)
			if err := os.Symlink(h.Linkname, dst); err != nil {
				return err
			}
		}
	}
}

func mustRel(base, target string) string {
	r, err := filepath.Rel(base, target)
	if err != nil {
		return "../invalid"
	}
	return r
}

func writeFile(dst string, r io.Reader, mode os.FileMode) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// CreateTarGz writes the given top-level entries of dir into a tar.gz at dst.
// Paths (slash-separated, relative to dir) for which skip returns true are
// left out, along with everything under them. prog, if set, follows how many
// bytes of file data have been written.
func CreateTarGz(dst, dir string, entries []string, skip SkipFunc, prog ProgressFunc) error {
	c := &counter{fn: prog}
	if prog != nil {
		c.total = treeSize(dir, entries, skip)
	}
	tmp := dst + ".partial"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	werr := func() error {
		for _, e := range entries {
			root := filepath.Join(dir, e)
			err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					// Files can vanish while the server runs; skip them.
					if os.IsNotExist(err) {
						return nil
					}
					return err
				}
				if !info.Mode().IsRegular() && !info.IsDir() {
					return nil
				}
				rel, err := filepath.Rel(dir, path)
				if err != nil {
					return err
				}
				if skip != nil && skip(filepath.ToSlash(rel)) {
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				h, err := tar.FileInfoHeader(info, "")
				if err != nil {
					return err
				}
				h.Name = filepath.ToSlash(rel)
				if info.IsDir() {
					h.Name += "/"
					return tw.WriteHeader(h)
				}
				f, err := os.Open(path)
				if err != nil {
					if os.IsNotExist(err) {
						return nil
					}
					return err
				}
				defer f.Close()
				// Write exactly the size recorded in the header even if the
				// file is being appended to.
				if err := tw.WriteHeader(h); err != nil {
					return err
				}
				n, err := io.CopyN(tw, f, h.Size)
				if err == io.EOF && n < h.Size {
					_, err = io.CopyN(tw, zeroReader{}, h.Size-n)
				}
				c.add(h.Size)
				return err
			})
			if err != nil {
				return err
			}
		}
		if err := tw.Close(); err != nil {
			return err
		}
		return gz.Close()
	}()
	if cerr := out.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmp)
		return werr
	}
	return os.Rename(tmp, dst)
}

// treeSize adds up the regular files CreateTarGz would include.
func treeSize(dir string, entries []string, skip SkipFunc) int64 {
	var total int64
	for _, e := range entries {
		filepath.Walk(filepath.Join(dir, e), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if rel, err := filepath.Rel(dir, path); err == nil && skip != nil && skip(filepath.ToSlash(rel)) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if info.Mode().IsRegular() {
				total += info.Size()
			}
			return nil
		})
	}
	return total
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// TopLevel lists the distinct top-level entry names in a tar.gz.
func TopLevel(src string) ([]string, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	var out []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		top, _, _ := strings.Cut(strings.TrimPrefix(h.Name, "./"), "/")
		if top != "" && !seen[top] {
			seen[top] = true
			out = append(out, top)
		}
	}
}
