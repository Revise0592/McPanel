// Package players reads and edits whitelist, operator and ban lists for
// Java and Bedrock servers.
package players

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/props"
	"github.com/Revise0592/McPanel/internal/store"
)

// List kinds.
const (
	Whitelist = "whitelist"
	Ops       = "ops"
	Bans      = "bans"
)

var javaName = regexp.MustCompile(`^[A-Za-z0-9_]{1,16}$`)

// ValidName checks a player name for the server type.
func ValidName(typ, name string) bool {
	if store.IsJava(typ) {
		return javaName.MatchString(name)
	}
	return name != "" && len(name) <= 32 && !strings.ContainsAny(name, "\"\n\r")
}

func file(typ, kind string) string {
	if typ == store.TypeBedrock {
		switch kind {
		case Whitelist:
			return "allowlist.json"
		case Ops:
			return "permissions.json"
		}
		return ""
	}
	switch kind {
	case Whitelist:
		return "whitelist.json"
	case Ops:
		return "ops.json"
	case Bans:
		return "banned-players.json"
	}
	return ""
}

// Read returns the names on a list.
func Read(def store.Server, dir, kind string) ([]string, error) {
	f := file(def.Type, kind)
	if f == "" {
		return nil, nil
	}
	b, err := os.ReadFile(filepath.Join(dir, f))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []map[string]any
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if def.Type == store.TypeBedrock && kind == Ops {
			if p, _ := e["permission"].(string); p != "operator" {
				continue
			}
			x, _ := e["xuid"].(string)
			out = append(out, x)
			continue
		}
		if n, _ := e["name"].(string); n != "" {
			out = append(out, n)
		}
	}
	slices.SortFunc(out, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return out, nil
}

// WhitelistEnabled reports whether the server only lets listed players in.
func WhitelistEnabled(def store.Server, dir string) bool {
	f, err := props.Load(filepath.Join(dir, "server.properties"))
	if err != nil {
		return false
	}
	return f.GetOr(whitelistKey(def.Type), "false") == "true"
}

func whitelistKey(typ string) string {
	if typ == store.TypeBedrock {
		return "allow-list"
	}
	return "white-list"
}

// SetWhitelistEnabled turns the whitelist on or off.
func SetWhitelistEnabled(def store.Server, inst *mc.Instance, on bool) error {
	if inst.Status().State == mc.Running {
		word := "off"
		if on {
			word = "on"
		}
		if store.IsJava(def.Type) {
			return inst.Send("whitelist " + word)
		}
		// Bedrock has no runtime toggle; edit the file and reload the list.
		defer inst.Send("allowlist reload")
	}
	f, err := props.Load(filepath.Join(inst.Dir(), "server.properties"))
	if err != nil {
		return err
	}
	v := "false"
	if on {
		v = "true"
	}
	f.Set(whitelistKey(def.Type), v)
	if store.IsJava(def.Type) {
		f.Set("enforce-whitelist", v)
	}
	return f.Save()
}

// Change adds or removes a player on a list. Running servers get a console
// command; stopped ones have their files edited directly.
func Change(def store.Server, inst *mc.Instance, kind, name string, add bool) error {
	if !ValidName(def.Type, name) {
		return errors.New("that doesn't look like a valid player name")
	}
	if inst.Status().State == mc.Running {
		return inst.Send(command(def.Type, kind, name, add))
	}
	if def.Type == store.TypeBedrock {
		if kind != Whitelist {
			return errors.New("start the server to change this list")
		}
		return editBedrockAllowlist(inst.Dir(), name, add)
	}
	return editJava(def, inst.Dir(), kind, name, add)
}

func command(typ, kind, name string, add bool) string {
	q := name
	if strings.Contains(name, " ") {
		q = `"` + name + `"`
	}
	switch {
	case typ == store.TypeBedrock && kind == Whitelist:
		if add {
			return "allowlist add " + q
		}
		return "allowlist remove " + q
	case kind == Whitelist:
		if add {
			return "whitelist add " + q
		}
		return "whitelist remove " + q
	case kind == Ops:
		if add {
			return "op " + q
		}
		return "deop " + q
	case kind == Bans && typ == store.TypeBedrock:
		// Bedrock has no ban list; kick instead.
		return "kick " + q
	case kind == Bans:
		if add {
			return "ban " + q
		}
		return "pardon " + q
	}
	return ""
}

// Kick removes a player from the game.
func Kick(inst *mc.Instance, name string) error {
	if strings.Contains(name, " ") {
		name = `"` + name + `"`
	}
	return inst.Send("kick " + name)
}

func editBedrockAllowlist(dir, name string, add bool) error {
	path := filepath.Join(dir, "allowlist.json")
	var entries []map[string]any
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &entries)
	}
	entries = slices.DeleteFunc(entries, func(e map[string]any) bool {
		n, _ := e["name"].(string)
		return strings.EqualFold(n, name)
	})
	if add {
		entries = append(entries, map[string]any{"ignoresPlayerLimit": false, "name": name})
	}
	return writeJSON(path, entries)
}

func editJava(def store.Server, dir, kind, name string, add bool) error {
	path := filepath.Join(dir, file(def.Type, kind))
	var entries []map[string]any
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &entries)
	}
	entries = slices.DeleteFunc(entries, func(e map[string]any) bool {
		n, _ := e["name"].(string)
		return strings.EqualFold(n, name)
	})
	if add {
		id, realName, err := lookupUUID(dir, name)
		if err != nil {
			return err
		}
		e := map[string]any{"uuid": id, "name": realName}
		switch kind {
		case Ops:
			e["level"] = 4
			e["bypassesPlayerLimit"] = false
		case Bans:
			e["created"] = time.Now().Format("2006-01-02 15:04:05 -0700")
			e["source"] = "McPanel"
			e["expires"] = "forever"
			e["reason"] = "Banned by an operator."
		}
		entries = append(entries, e)
	}
	return writeJSON(path, entries)
}

// lookupUUID finds a Java player's UUID: from Mojang for online-mode servers,
// or derived from the name for offline-mode ones.
func lookupUUID(dir, name string) (string, string, error) {
	f, _ := props.Load(filepath.Join(dir, "server.properties"))
	if f != nil && f.GetOr("online-mode", "true") == "false" {
		return offlineUUID(name), name, nil
	}
	var p struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := install.GetJSON(ctx, "https://api.mojang.com/users/profiles/minecraft/"+name, &p); err != nil || len(p.ID) != 32 {
		return "", "", fmt.Errorf("couldn't find a Minecraft account called %s", name)
	}
	u := p.ID
	return u[0:8] + "-" + u[8:12] + "-" + u[12:16] + "-" + u[16:20] + "-" + u[20:], p.Name, nil
}

func offlineUUID(name string) string {
	h := md5.Sum([]byte("OfflinePlayer:" + name))
	h[6] = h[6]&0x0f | 0x30
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func writeJSON(path string, entries []map[string]any) error {
	if entries == nil {
		entries = []map[string]any{}
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
