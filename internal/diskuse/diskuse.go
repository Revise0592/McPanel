// Package diskuse measures what takes up space in a server's folder and
// clears out old logs.
package diskuse

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Revise0592/McPanel/internal/addons"
	"github.com/Revise0592/McPanel/internal/backup"
)

// Part is one kind of file and how much space it takes.
type Part struct {
	Label string
	Size  int64
}

// Report is a server's disk use.
type Report struct {
	// Parts are the server folder's contents, biggest first.
	Parts []Part
	Total int64
	// Backups is the server's backups folder, kept outside the server
	// folder.
	Backups int64
	// OldLogs is how much ClearOldLogs would free.
	OldLogs int64
}

// logDirs hold files the server only writes, never reads back.
var logDirs = []string{"logs", "crash-reports"}

// oldLog reports whether a file in a log folder is safe to delete: every
// file except the one being written now.
func oldLog(rel string) bool {
	base := filepath.Base(rel)
	return base != "latest.log" && base != "debug.log"
}

// Measure adds up the server's files by kind.
func Measure(typ, serverDir, backupDir string) Report {
	kind := map[string]string{}
	for _, w := range backup.WorldFolders(typ, serverDir) {
		kind[w] = "World"
	}
	if addons.Supported(typ) {
		kind[addons.Folder(typ)] = strings.ToUpper(addons.Noun(typ)[:1]) + addons.Noun(typ)[1:] + "s"
	}
	for _, d := range logDirs {
		kind[d] = "Logs and crash reports"
	}
	sizes := map[string]int64{}
	var r Report
	filepath.WalkDir(serverDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(serverDir, p)
		top, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
		label, ok := kind[top]
		if !ok {
			label = "Server software and settings"
		}
		sizes[label] += info.Size()
		r.Total += info.Size()
		if label == "Logs and crash reports" && top != rel && oldLog(rel) {
			r.OldLogs += info.Size()
		}
		return nil
	})
	for l, n := range sizes {
		r.Parts = append(r.Parts, Part{Label: l, Size: n})
	}
	slices.SortFunc(r.Parts, func(a, b Part) int {
		if a.Size != b.Size {
			if a.Size > b.Size {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Label, b.Label)
	})
	filepath.WalkDir(backupDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				r.Backups += info.Size()
			}
		}
		return nil
	})
	return r
}

// ClearOldLogs deletes old and compressed logs and crash reports, keeping
// the log being written now. It returns how much space was freed.
func ClearOldLogs(serverDir string) (int64, error) {
	var freed int64
	var errs []error
	for _, d := range logDirs {
		filepath.WalkDir(filepath.Join(serverDir, d), func(p string, e fs.DirEntry, err error) error {
			if err != nil || !e.Type().IsRegular() || !oldLog(p) {
				return nil
			}
			info, err := e.Info()
			if err != nil {
				return nil
			}
			if err := os.Remove(p); err != nil {
				errs = append(errs, err)
				return nil
			}
			freed += info.Size()
			return nil
		})
	}
	return freed, errors.Join(errs...)
}
