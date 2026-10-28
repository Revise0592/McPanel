// Package backup creates, lists, restores and schedules world backups.
package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/archive"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/props"
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
// If the panel has a copy folder, the backup is then copied there too.
// The result is noted in the server's activity, except when the server was
// busy with another job.
func Create(dataDir string, inst *mc.Instance, label string) (Backup, error) {
	b, err := create(dataDir, inst, label)
	if errors.Is(err, mc.ErrBusy) {
		return b, err
	}
	if err != nil {
		inst.Record(activity.Failed, "Backup failed: %v", err)
		return b, err
	}
	inst.Logf("Backup saved (%s)", HumanSize(b.Size))
	inst.Manager().Activity().Add(inst.ID, activity.Event{Text: fmt.Sprintf("%s backup saved (%s)", KindLabel(label), HumanSize(b.Size))})
	if to := inst.Manager().Settings().BackupCopyDir; to != "" {
		if err := copyOut(dataDir, inst.ID, b.Name, to); err != nil {
			inst.Record(activity.Failed, "Couldn't copy the backup to %s: %v", to, err)
		}
	}
	return b, nil
}

// KindLabel names a backup label for display.
func KindLabel(label string) string {
	switch label {
	case "auto":
		return "Scheduled"
	case "manual":
		return "Manual"
	case "before-restore":
		return "Before restore"
	case "before-update":
		return "Before update"
	}
	return label
}

func create(dataDir string, inst *mc.Instance, label string) (Backup, error) {
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
	SaveInfo(dataDir, def)
	now := time.Now()
	name := now.Format(timeFormat)
	if label != "" {
		name += "-" + label
	}
	name += ".tar.gz"
	dst := filepath.Join(Dir(dataDir, def.ID), name)
	if err := archive.CreateTarGz(dst, inst.Dir(), ents, skipNested, inst.SetProgress); err != nil {
		return Backup{}, err
	}
	info, err := os.Stat(dst)
	if err != nil {
		return Backup{}, err
	}
	return Backup{Name: name, Label: label, Size: info.Size(), Created: now}, nil
}

// WorldFolders are the top-level folders that hold a server's world: the
// level-name folder and its Nether and End for Java, "worlds" for Bedrock.
func WorldFolders(typ, serverDir string) []string {
	if typ == store.TypeBedrock {
		return []string{"worlds"}
	}
	world := "world"
	if f, err := props.Load(filepath.Join(serverDir, "server.properties")); err == nil {
		world = f.GetOr("level-name", world)
	}
	return []string{world, world + "_nether", world + "_the_end"}
}

// Restore replaces the server's files with a backup. With worldOnly, just
// the world folders are replaced and plugins, configs and the rest are left
// alone. The server is stopped first, and a safety backup of the current
// state is made.
func Restore(dataDir string, inst *mc.Instance, name string, worldOnly bool) error {
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
	if worldOnly {
		def, err := inst.Def()
		if err != nil {
			return err
		}
		worlds := WorldFolders(def.Type, inst.Dir())
		tops = slices.DeleteFunc(tops, func(t string) bool { return !slices.Contains(worlds, t) })
		if len(tops) == 0 {
			return errors.New("this backup has no world folder")
		}
	}
	tmp := filepath.Join(inst.Dir(), ".mcpanel-restore")
	os.RemoveAll(tmp)
	if err := archive.ExtractTarGz(src, tmp, 0, inst.SetProgress); err != nil {
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
	if worldOnly {
		inst.Record(activity.Info, "Restored the world from backup %s", strings.TrimSuffix(name, ".tar.gz"))
	} else {
		inst.Record(activity.Info, "Restored backup from %s", strings.TrimSuffix(name, ".tar.gz"))
	}
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

// infoFile records which server a backup folder belongs to, so its backups
// can still be named after the server is deleted.
const infoFile = "server.json"

type info struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Version string `json:"version"`
}

// SaveInfo writes the server's name and type into its backup folder, if
// the folder exists. Failure only costs a nicer name later, so it's ignored.
func SaveInfo(dataDir string, sv store.Server) {
	dir := Dir(dataDir, sv.ID)
	if _, err := os.Stat(dir); err != nil {
		return
	}
	b, _ := json.Marshal(info{Name: sv.Name, Type: sv.Type, Version: sv.Version})
	os.WriteFile(filepath.Join(dir, infoFile), b, 0o644)
}

// Orphan is the backup folder of a server that no longer exists.
type Orphan struct {
	ID      string
	Name    string
	Type    string // empty when unknown
	Version string
	Backups []Backup
	Size    int64
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// trailing random part of a server ID, e.g. "-a1b2c3"
var idSuffix = regexp.MustCompile(`-[0-9a-f]{6}$`)

// Orphans lists backup folders whose server isn't in known, largest first.
// Folders without any backups are skipped.
func Orphans(dataDir string, known func(id string) bool) ([]Orphan, error) {
	ents, err := os.ReadDir(filepath.Join(dataDir, "backups"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Orphan
	for _, e := range ents {
		id := e.Name()
		if !e.IsDir() || !idRe.MatchString(id) || known(id) {
			continue
		}
		list, err := List(dataDir, id)
		if err != nil || len(list) == 0 {
			continue
		}
		o := Orphan{ID: id, Backups: list}
		for _, b := range list {
			o.Size += b.Size
		}
		var in info
		if b, err := os.ReadFile(filepath.Join(Dir(dataDir, id), infoFile)); err == nil && json.Unmarshal(b, &in) == nil && in.Name != "" {
			o.Name, o.Type, o.Version = in.Name, in.Type, in.Version
		} else {
			// Older folders have no info file; the ID starts with the name.
			o.Name = strings.ReplaceAll(idSuffix.ReplaceAllString(id, ""), "-", " ")
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out, nil
}

// DeleteFolder removes a server's whole backup folder.
func DeleteFolder(dataDir, id string) error {
	if !idRe.MatchString(id) {
		return errors.New("invalid server ID")
	}
	return os.RemoveAll(Dir(dataDir, id))
}

// prune deletes the oldest automatic backups beyond keep, along with their
// copies in copyDir if there is one.
func prune(dataDir, copyDir, id string, keep int) {
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
			if copyDir != "" {
				os.Remove(filepath.Join(copyDir, id, b.Name))
			}
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
				continue
			}
			prune(m.DataDir(), st.Settings().BackupCopyDir, sv.ID, sv.BackupKeep)
		}
	}
}

// copyOut copies a backup into dir/<server ID>/, writing to a temporary
// file first so an interrupted copy never looks like a finished backup.
func copyOut(dataDir, id, name, dir string) error {
	dst := filepath.Join(dir, id)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	if b, err := os.ReadFile(filepath.Join(Dir(dataDir, id), infoFile)); err == nil {
		os.WriteFile(filepath.Join(dst, infoFile), b, 0o644)
	}
	in, err := os.Open(filepath.Join(Dir(dataDir, id), name))
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := filepath.Join(dst, "."+name+".part")
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(dst, name))
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// CheckCopyDir reports why dir can't hold backup copies, or nil if it can.
func CheckCopyDir(dataDir, dir string) error {
	if !filepath.IsAbs(dir) {
		return errors.New("use a full path, starting with /")
	}
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return errors.New("that folder doesn't exist")
	}
	if rel, err := filepath.Rel(dataDir, dir); err == nil && !strings.HasPrefix(rel, "..") {
		return errors.New("choose a folder outside McPanel's data folder")
	}
	f, err := os.CreateTemp(dir, ".mcpanel-check-*")
	if err != nil {
		return errors.New("McPanel can't write to that folder")
	}
	f.Close()
	os.Remove(f.Name())
	return nil
}

// FreeSpace is how many bytes are free on the disk holding dir.
func FreeSpace(dir string) (int64, bool) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(dir, &fs); err != nil {
		return 0, false
	}
	return int64(fs.Bavail) * int64(fs.Bsize), true
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
