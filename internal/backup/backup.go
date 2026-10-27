// Package backup creates, lists, restores and schedules world backups.
package backup

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/archive"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/store"
)

// Backup is one archive on disk.
type Backup struct {
	Name    string
	Label   string
	Size    int64
	Created time.Time
}

// Auto reports whether the backup was made by the schedule.
func (b Backup) Auto() bool { return b.Label == "auto" }

const timeFormat = "20060102-150405"

var nameRe = regexp.MustCompile(`^(\d{8}-\d{6})(?:-([a-z0-9-]+))?\.tar\.gz$`)

// Dir is where a server's backups live.
func Dir(dataDir, id string) string { return filepath.Join(dataDir, "backups", id) }

// Path returns the file path for a named backup, rejecting bad names.
func Path(dataDir, id, name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", errors.New("invalid backup name")
	}
	return filepath.Join(Dir(dataDir, id), name), nil
}

// List returns a server's backups, newest first.
func List(dataDir, id string) ([]Backup, error) {
	ents, err := os.ReadDir(Dir(dataDir, id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Backup
	for _, e := range ents {
		m := nameRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		t, err := time.ParseInLocation(timeFormat, m[1], time.Local)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Backup{Name: e.Name(), Label: m[2], Size: info.Size(), Created: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// entries lists the top-level files and folders worth backing up.
func entries(typ, dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if typ == store.TypeBedrock {
			switch n {
			case "worlds", "server.properties", "allowlist.json", "permissions.json":
				out = append(out, n)
			}
			continue
		}
		switch n {
		case "libraries", "versions", "cache", "logs", "crash-reports", ".fabric", "bundler":
			continue
		}
		if !e.IsDir() && strings.HasSuffix(n, ".jar") {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

// skipNested drops caches that servers and plugins rebuild on their own.
func skipNested(rel string) bool {
	switch rel {
	case "plugins/.paper-remapped", "plugins/spark/tmp", "plugins/bStats", ".mcpanel-restore":
		return true
	}
	return false
}

var saveDone = func(l string) bool {
	return strings.Contains(l, "Saved the game") || strings.Contains(l, "Saved the world")
}

// Create backs up a server. label is a short tag such as "auto" or "manual".
// A running server keeps running; saving is paused while files are copied.
func Create(dataDir string, inst *mc.Instance, label string) (Backup, error) {
	def, err := inst.Def()
	if err != nil {
		return Backup{}, err
	}
	if !def.Installed {
		return Backup{}, errors.New("the server isn't installed yet")
	}
	if err := inst.BeginTask("Backing up"); err != nil {
		return Backup{}, err
	}
	defer inst.EndTask()

	if inst.Status().State == mc.Running {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if store.IsJava(def.Type) {
			inst.Send("save-off")
			if err := inst.SendAndWait(ctx, "save-all flush", saveDone); err != nil {
				inst.Send("save-on")
				return Backup{}, fmt.Errorf("server did not save: %w", err)
			}
			defer inst.Send("save-on")
		} else {
			if err := inst.Send("save hold"); err != nil {
				return Backup{}, err
			}
			defer inst.Send("save resume")
			ready := false
			for !ready {
				qctx, qcancel := context.WithTimeout(ctx, 3*time.Second)
				err := inst.SendAndWait(qctx, "save query", func(l string) bool {
					return strings.Contains(l, "Files are now ready to be copied")
				})
				qcancel()
				if err == nil {
					ready = true
				} else if ctx.Err() != nil {
					return Backup{}, errors.New("server did not get ready for backup")
				}
			}
		}
	}

	ents, err := entries(def.Type, inst.Dir())
	if err != nil {
		return Backup{}, err
	}
	if len(ents) == 0 {
		return Backup{}, errors.New("nothing to back up yet")
	}
	if err := os.MkdirAll(Dir(dataDir, def.ID), 0o755); err != nil {
		return Backup{}, err
	}
	now := time.Now()
	name := now.Format(timeFormat)
	if label != "" {
		name += "-" + label
	}
	name += ".tar.gz"
	dst := filepath.Join(Dir(dataDir, def.ID), name)
	if err := archive.CreateTarGz(dst, inst.Dir(), ents, skipNested); err != nil {
		return Backup{}, err
	}
	info, err := os.Stat(dst)
	if err != nil {
		return Backup{}, err
	}
	inst.Logf("Backup saved (%s)", HumanSize(info.Size()))
	return Backup{Name: name, Label: label, Size: info.Size(), Created: now}, nil
}

// Restore replaces the server's files with a backup. The server is stopped
// first, and a safety backup of the current state is made.
func Restore(dataDir string, inst *mc.Instance, name string) error {
	src, err := Path(dataDir, inst.ID, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(src); err != nil {
		return err
	}
	wasOnline := inst.Status().Online()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := inst.StopAndWait(ctx); err != nil {
		return err
	}
	if _, err := Create(dataDir, inst, "before-restore"); err != nil {
		return fmt.Errorf("safety backup failed: %w", err)
	}
	if err := inst.BeginTask("Restoring"); err != nil {
		return err
	}
	defer inst.EndTask()

	tops, err := archive.TopLevel(src)
	if err != nil {
		return err
	}
	tmp := filepath.Join(inst.Dir(), ".mcpanel-restore")
	os.RemoveAll(tmp)
	if err := archive.ExtractTarGz(src, tmp, 0); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	for _, t := range tops {
		dst := filepath.Join(inst.Dir(), t)
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(tmp, t), dst); err != nil {
			return err
		}
	}
	os.RemoveAll(tmp)
	inst.Logf("Restored backup from %s", strings.TrimSuffix(name, ".tar.gz"))
	inst.EndTask()
	if wasOnline {
		return inst.Start()
	}
	return nil
}

// Delete removes a backup.
func Delete(dataDir, id, name string) error {
	p, err := Path(dataDir, id, name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// prune deletes the oldest automatic backups beyond keep.
func prune(dataDir, id string, keep int) {
	if keep <= 0 {
		return
	}
	list, err := List(dataDir, id)
	if err != nil {
		return
	}
	n := 0
	for _, b := range list {
		if !b.Auto() {
			continue
		}
		n++
		if n > keep {
			os.Remove(filepath.Join(Dir(dataDir, id), b.Name))
		}
	}
}

// RunScheduler makes automatic backups until ctx is cancelled. Servers that
// nobody has played on since their last backup are skipped.
func RunScheduler(ctx context.Context, st *store.Store, m *mc.Manager) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		for _, sv := range st.Servers() {
			if sv.BackupEveryHours <= 0 || !sv.Installed {
				continue
			}
			list, _ := List(m.DataDir(), sv.ID)
			var last time.Time
			for _, b := range list {
				if b.Auto() {
					last = b.Created
					break
				}
			}
			if time.Since(last) < time.Duration(sv.BackupEveryHours)*time.Hour {
				continue
			}
			inst, err := m.Get(sv.ID)
			if err != nil {
				continue
			}
			if !last.IsZero() && inst.LastPlayerActivity().Before(last) {
				continue
			}
			if _, err := Create(m.DataDir(), inst, "auto"); err != nil {
				log.Printf("backup %s: %v", sv.Name, err)
				continue
			}
			prune(m.DataDir(), sv.ID, sv.BackupKeep)
		}
	}
}

// HumanSize formats a byte count.
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
