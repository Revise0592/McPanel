package modpack

import (
	"context"
	"slices"
	"strings"

	"github.com/Revise0592/McPanel/internal/install"
)

// Packs mark each file as needed on servers or not, and those marks are
// sometimes wrong: a library a server mod needs is marked client-only, and
// the server won't start without it. Launchers never notice, since players'
// computers get every file. So before downloading, McPanel looks the pack's
// files up on Modrinth and brings back any skipped file that a server mod
// requires.

// fileVersion is what Modrinth knows about one file in a pack.
type fileVersion struct {
	Name         string `json:"name"`
	ProjectID    string `json:"project_id"`
	Dependencies []struct {
		ProjectID      string `json:"project_id"`
		DependencyType string `json:"dependency_type"`
	} `json:"dependencies"`
}

// lookupFiles asks Modrinth about files by their SHA-512 hashes. Files it
// doesn't know are missing from the result. Tests replace it.
var lookupFiles = realLookup

func realLookup(ctx context.Context, hashes []string) (map[string]fileVersion, error) {
	out := map[string]fileVersion{}
	for chunk := range slices.Chunk(hashes, 500) {
		var res map[string]fileVersion
		body := map[string]any{"hashes": chunk, "algorithm": "sha512"}
		if err := install.PostJSON(ctx, api+"/version_files", body, &res); err != nil {
			return nil, err
		}
		for h, v := range res {
			out[h] = v
		}
	}
	return out, nil
}

func hashOf(f File) string { return strings.ToLower(f.Hashes["sha512"]) }

// addRequired moves skipped files that server files need, directly or
// through other needed files, onto the server's list. If Modrinth can't be
// reached the pack is used as it is.
func addRequired(ctx context.Context, p Pack, logf install.Logf) Pack {
	if len(p.client) == 0 {
		return p
	}
	var hashes []string
	for _, f := range append(slices.Clone(p.server), p.client...) {
		if h := hashOf(f); h != "" {
			hashes = append(hashes, h)
		}
	}
	known, err := lookupFiles(ctx, hashes)
	if err != nil {
		logf("Couldn't check the pack's client-only files against Modrinth (%v); using the pack's own list", err)
		return p
	}
	// Skipped files by the Modrinth project they belong to.
	skipped := map[string][]int{}
	for i, f := range p.client {
		if v, ok := known[hashOf(f)]; ok {
			skipped[v.ProjectID] = append(skipped[v.ProjectID], i)
		}
	}
	moved := map[int]bool{}
	queue := slices.Clone(p.server)
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		v, ok := known[hashOf(f)]
		if !ok {
			continue
		}
		for _, d := range v.Dependencies {
			if d.DependencyType != "required" {
				continue
			}
			for _, i := range skipped[d.ProjectID] {
				c := p.client[i]
				if p.rescued == nil {
					p.rescued = map[string][]string{}
				}
				if moved[i] {
					p.rescued[c.Path] = append(p.rescued[c.Path], f.Path)
					continue
				}
				if err := safe(c); err != nil {
					logf("%s needs %s, but it can't be installed: %v", v.Name, c.Path, err)
					continue
				}
				moved[i] = true
				p.rescued[c.Path] = append(p.rescued[c.Path], f.Path)
				queue = append(queue, c)
				logf("Also installing %s: the pack marks it as only for players' computers, but %s needs it", c.Path, v.Name)
			}
		}
	}
	if len(moved) == 0 {
		return p
	}
	var client []File
	for i, f := range p.client {
		if moved[i] {
			p.server = append(p.server, f)
		} else {
			client = append(client, f)
		}
	}
	p.client = client
	p.count()
	return p
}
