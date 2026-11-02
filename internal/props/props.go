// Package props reads and edits server.properties files, keeping comments,
// ordering and untouched lines exactly as they were.
package props

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"
)

// File is a parsed properties file.
type File struct {
	path  string
	lines []string
}

// Load reads path. A missing file yields an empty File.
func Load(path string) (*File, error) {
	f := &File{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s != "" {
		f.lines = strings.Split(s, "\n")
	}
	return f, nil
}

func parseLine(line string) (key, val string, ok bool) {
	t := strings.TrimSpace(line)
	if t == "" || t[0] == '#' || t[0] == '!' {
		return "", "", false
	}
	k, v, found := strings.Cut(t, "=")
	if !found {
		return "", "", false
	}
	return strings.TrimSpace(k), strings.TrimSpace(v), true
}

// Get returns the value for key.
func (f *File) Get(key string) (string, bool) {
	for _, l := range f.lines {
		if k, v, ok := parseLine(l); ok && k == key {
			return v, true
		}
	}
	return "", false
}

// GetOr returns the value for key or def.
func (f *File) GetOr(key, def string) string {
	if v, ok := f.Get(key); ok {
		return v
	}
	return def
}

// Set changes key in place or appends it. It reports whether anything changed.
func (f *File) Set(key, val string) bool {
	val = strings.ReplaceAll(strings.ReplaceAll(val, "\n", " "), "\r", "")
	for i, l := range f.lines {
		if k, v, ok := parseLine(l); ok && k == key {
			if v == val {
				return false
			}
			f.lines[i] = key + "=" + val
			return true
		}
	}
	f.lines = append(f.lines, key+"="+val)
	return true
}

// Raw returns the full file text.
func (f *File) Raw() string {
	if len(f.lines) == 0 {
		return ""
	}
	return strings.Join(f.lines, "\n") + "\n"
}

// SetRaw replaces the whole file text.
func (f *File) SetRaw(s string) {
	s = strings.TrimSuffix(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	f.lines = nil
	if s != "" {
		f.lines = strings.Split(s, "\n")
	}
}

// Save writes the file.
func (f *File) Save() error {
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(f.Raw()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

// Unescape decodes a value as Java reads it: \uXXXX, \n, \t and friends.
// Minecraft writes § as §, so text read raw shows the escapes.
func Unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	var units []uint16 // pending \u escapes, so surrogate pairs join up
	flush := func() {
		if len(units) > 0 {
			b.WriteString(string(utf16.Decode(units)))
			units = units[:0]
		}
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			flush()
			b.WriteByte(s[i])
			continue
		}
		i++
		if s[i] == 'u' && i+4 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+5], 16, 16); err == nil {
				units = append(units, uint16(n))
				i += 4
				continue
			}
		}
		flush()
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'f':
			b.WriteByte('\f')
		default:
			b.WriteByte(s[i])
		}
	}
	flush()
	return b.String()
}

// Escape encodes a value so Java reads it back exactly: newlines as \n,
// leading spaces kept, and anything outside ASCII as \uXXXX, which is how
// Minecraft writes the file itself.
func Escape(s string) string {
	var b strings.Builder
	lead := true
	for _, r := range s {
		switch {
		case r == ' ' && lead:
			b.WriteString(`\ `)
			continue
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r > 0x7e:
			for _, u := range utf16.Encode([]rune{r}) {
				fmt.Fprintf(&b, `\u%04X`, u)
			}
		default:
			b.WriteRune(r)
		}
		lead = false
	}
	return b.String()
}
