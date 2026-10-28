package players

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Revise0592/McPanel/internal/store"
)

// Recent is a player who has been on the server, and when they were last
// seen there.
type Recent struct {
	Name string
	Seen time.Time
}

// RecentPlayers lists players who have joined, most recent first, leaving
// out those online now. Java servers keep usercache.json, which says when
// each player last joined; seen adds what the panel has watched itself,
// which is all there is for Bedrock.
func RecentPlayers(def store.Server, dir string, seen map[string]time.Time, online []string, limit int) []Recent {
	last := map[string]time.Time{}
	if store.IsJava(def.Type) {
		for name, t := range userCache(dir) {
			last[name] = t
		}
	}
	for name, t := range seen {
		if t.After(last[name]) {
			last[name] = t
		}
	}
	var out []Recent
	for name, t := range last {
		if !slices.Contains(online, name) {
			out = append(out, Recent{name, t})
		}
	}
	slices.SortFunc(out, func(a, b Recent) int { return b.Seen.Compare(a.Seen) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// userCache reads when each player last joined from usercache.json. The
// server sets an entry to expire a month after the player's last join.
func userCache(dir string) map[string]time.Time {
	b, err := os.ReadFile(filepath.Join(dir, "usercache.json"))
	if err != nil {
		return nil
	}
	var entries []struct {
		Name      string `json:"name"`
		ExpiresOn string `json:"expiresOn"`
	}
	if json.Unmarshal(b, &entries) != nil {
		return nil
	}
	out := map[string]time.Time{}
	for _, e := range entries {
		t, err := time.Parse("2006-01-02 15:04:05 -0700", e.ExpiresOn)
		if err != nil || !javaName.MatchString(e.Name) {
			continue
		}
		out[e.Name] = t.AddDate(0, -1, 0)
	}
	return out
}
