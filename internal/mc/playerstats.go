package mc

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"time"
)

// PlayerStats is what the panel has seen of one player on a server. It is
// kept on disk, so it lasts across panel restarts.
type PlayerStats struct {
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	// Played is the total time online, including the current session.
	Played time.Duration `json:"played"`
}

func statsPath(dataDir, id string) string { return filepath.Join(dataDir, "players", id+".json") }

func (i *Instance) loadStats() {
	if i.m == nil {
		return
	}
	if b, err := os.ReadFile(statsPath(i.m.dataDir, i.ID)); err == nil {
		json.Unmarshal(b, &i.stats)
	}
}

// joinedLocked notes a player coming online.
func (i *Instance) joinedLocked(name string, now time.Time) {
	if _, on := i.players[name]; !on {
		i.players[name] = now
	}
	st := i.stats[name]
	if st.FirstSeen.IsZero() {
		st.FirstSeen = now
	}
	st.LastSeen = now
	i.stats[name] = st
	i.statsDirty = true
}

// leftLocked notes a player going offline and adds the session to their
// play time.
func (i *Instance) leftLocked(name string, now time.Time) {
	st := i.stats[name]
	if joined, on := i.players[name]; on {
		st.Played += now.Sub(joined)
		delete(i.players, name)
	}
	st.LastSeen = now
	i.stats[name] = st
	i.statsDirty = true
}

// saveStats writes the player stats if they have changed.
func (i *Instance) saveStats() {
	i.mu.Lock()
	if !i.statsDirty || i.m == nil {
		i.mu.Unlock()
		return
	}
	b, err := json.Marshal(i.stats)
	i.statsDirty = false
	i.mu.Unlock()
	if err != nil {
		return
	}
	i.saveMu.Lock()
	defer i.saveMu.Unlock()
	p := statsPath(i.m.dataDir, i.ID)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return
	}
	if os.WriteFile(p+".tmp", b, 0o640) == nil {
		os.Rename(p+".tmp", p)
	}
}

// PlayerStats returns what the panel has seen of each player. Players
// online now count as seen now, with their current session in Played.
func (i *Instance) PlayerStats() map[string]PlayerStats {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := maps.Clone(i.stats)
	now := time.Now()
	for name, joined := range i.players {
		st := out[name]
		st.LastSeen = now
		st.Played += now.Sub(joined)
		out[name] = st
	}
	return out
}
