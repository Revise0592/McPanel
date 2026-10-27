// Package store persists panel settings and server definitions to a single
// JSON file in the data directory.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Server types.
const (
	TypeVanilla = "vanilla"
	TypePaper   = "paper"
	TypeFabric  = "fabric"
	TypeBedrock = "bedrock"
)

// TypeLabel returns a display name for a server type.
func TypeLabel(t string) string {
	switch t {
	case TypeVanilla:
		return "Vanilla"
	case TypePaper:
		return "Paper"
	case TypeFabric:
		return "Fabric"
	case TypeBedrock:
		return "Bedrock"
	}
	return t
}

// IsJava reports whether the server type runs on the JVM.
func IsJava(t string) bool { return t != TypeBedrock }

// Server is a saved server definition.
type Server struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	Version          string `json:"version"`
	Build            string `json:"build,omitempty"`
	Executable       string `json:"executable,omitempty"`
	JavaMajor        int    `json:"javaMajor,omitempty"`
	MemoryMB         int    `json:"memoryMB,omitempty"`
	JavaArgs         string `json:"javaArgs,omitempty"`
	AutoStart        bool   `json:"autoStart"`
	RestartOnCrash   bool   `json:"restartOnCrash"`
	BackupEveryHours int    `json:"backupEveryHours"`
	BackupKeep       int    `json:"backupKeep"`
	Installed        bool   `json:"installed"`
	// InitialProps are server.properties values chosen at creation, written
	// once the server files exist.
	InitialProps map[string]string `json:"initialProps,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
}

// DefaultUsername is given to panels set up before usernames existed.
const DefaultUsername = "admin"

// Settings holds panel-wide settings.
type Settings struct {
	PanelName     string `json:"panelName"`
	Accent        string `json:"accent"`
	Theme         string `json:"theme"`
	Username      string `json:"username,omitempty"`
	PasswordHash  string `json:"passwordHash,omitempty"`
	SessionSecret string `json:"sessionSecret"`
	// Two-factor sign-in. TOTPSecret is set once 2FA is on; TOTPPending
	// holds a secret being set up but not yet confirmed.
	TOTPSecret     string   `json:"totpSecret,omitempty"`
	TOTPPending    string   `json:"totpPending,omitempty"`
	TOTPLastStep   int64    `json:"totpLastStep,omitempty"`
	RecoveryHashes []string `json:"recoveryHashes,omitempty"`
}

// TwoFactor reports whether two-factor sign-in is on.
func (s Settings) TwoFactor() bool { return s.TOTPSecret != "" }

// DisableTwoFactor clears every two-factor setting.
func (s *Settings) DisableTwoFactor() {
	s.TOTPSecret, s.TOTPPending, s.TOTPLastStep, s.RecoveryHashes = "", "", 0, nil
}

type file struct {
	Settings Settings  `json:"settings"`
	Servers  []*Server `json:"servers"`
}

// Store is safe for concurrent use. Getters return copies.
type Store struct {
	mu   sync.Mutex
	path string
	data file
}

// ErrNotFound is returned when a server ID does not exist.
var ErrNotFound = errors.New("server not found")

// Open loads (or initialises) the store at dataDir/mcpanel.json.
func Open(dataDir string) (*Store, error) {
	s := &Store{path: filepath.Join(dataDir, "mcpanel.json")}
	b, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &s.data); err != nil {
			return nil, err
		}
	}
	changed := false
	if s.data.Settings.SessionSecret == "" {
		s.data.Settings.SessionSecret = RandomHex(32)
		changed = true
	}
	if s.data.Settings.PanelName == "" {
		s.data.Settings.PanelName = "McPanel"
		changed = true
	}
	if s.data.Settings.Accent == "" {
		s.data.Settings.Accent = "#3b82f6"
		changed = true
	}
	if s.data.Settings.Theme == "" {
		s.data.Settings.Theme = "system"
		changed = true
	}
	// Panels set up before usernames existed sign in as "admin", the name
	// their authenticator apps already show.
	if s.data.Settings.PasswordHash != "" && s.data.Settings.Username == "" {
		s.data.Settings.Username = DefaultUsername
		changed = true
	}
	if changed {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Settings returns a copy of the panel settings.
func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Settings
}

// UpdateSettings applies fn to the settings and saves.
func (s *Store) UpdateSettings(fn func(*Settings)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.data.Settings)
	return s.saveLocked()
}

// Servers returns copies of all servers sorted by name.
func (s *Store) Servers() []Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Server, 0, len(s.data.Servers))
	for _, sv := range s.data.Servers {
		out = append(out, *sv)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Server returns a copy of one server.
func (s *Store) Server(id string) (Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sv := range s.data.Servers {
		if sv.ID == id {
			return *sv, nil
		}
	}
	return Server{}, ErrNotFound
}

// AddServer assigns an ID and saves a new server.
func (s *Store) AddServer(sv Server) (Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sv.ID = slug(sv.Name) + "-" + RandomHex(3)
	sv.CreatedAt = time.Now()
	cp := sv
	s.data.Servers = append(s.data.Servers, &cp)
	return sv, s.saveLocked()
}

// UpdateServer applies fn to the server with the given ID and saves.
func (s *Store) UpdateServer(id string, fn func(*Server)) (Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sv := range s.data.Servers {
		if sv.ID == id {
			fn(sv)
			sv.ID = id
			return *sv, s.saveLocked()
		}
	}
	return Server{}, ErrNotFound
}

// DeleteServer removes a server definition.
func (s *Store) DeleteServer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sv := range s.data.Servers {
		if sv.ID == id {
			s.data.Servers = append(s.data.Servers[:i], s.data.Servers[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// RandomHex returns n random bytes hex-encoded.
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(name string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 24 {
		s = strings.Trim(s[:24], "-")
	}
	if s == "" {
		s = "server"
	}
	return s
}
