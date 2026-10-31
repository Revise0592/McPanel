// Package heads fetches Java players' skins from Mojang and turns them into
// small face icons. Icons are kept in memory only, so nothing is written to disk.
package heads

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Revise0592/McPanel/internal/install"
)

// Size is the width and height of a head icon in pixels.
const Size = 32

const (
	// keepFor is how long a fetched head is reused before asking Mojang again.
	// Players who join a server are refreshed sooner; see Forget.
	keepFor = time.Hour
	// retryAfter is how long a failed lookup shows the placeholder before trying again.
	retryAfter = time.Hour
	// retryStale is how long a head that failed to refresh keeps its old
	// picture before trying again.
	retryStale = 10 * time.Minute
	// rejoinWindow and rejoinKeep: Mojang can take a minute or two to hand
	// out a new skin, so a head fetched soon after a join is only kept briefly.
	rejoinWindow = 10 * time.Minute
	rejoinKeep   = 2 * time.Minute
	// maxEntries bounds the cache; past it, stale entries are dropped.
	maxEntries = 2000
	// maxSkin guards against oversized skin downloads.
	maxSkin = 1 << 20
)

var validName = regexp.MustCompile(`^[A-Za-z0-9_]{1,16}$`)

// Placeholder is the icon shown for unknown players and Bedrock players.
var Placeholder = placeholder()

// Fetcher downloads a URL. Tests replace it.
type Fetcher func(ctx context.Context, url string) ([]byte, error)

// Cache looks up and remembers head icons.
type Cache struct {
	Fetch Fetcher

	mu      sync.Mutex
	entries map[string]*entry
	joined  map[string]time.Time // when Forget last saw each player join
}

type entry struct {
	png   []byte
	until time.Time
	ready chan struct{} // closed once png is set
}

// New returns a cache that fetches from Mojang.
func New() *Cache { return &Cache{Fetch: get} }

// Get returns the head icon for a Java player name as PNG bytes. Unknown
// players, invalid names and lookup failures give the placeholder.
func (c *Cache) Get(ctx context.Context, name string) []byte {
	if !validName.MatchString(name) {
		return Placeholder
	}
	key := strings.ToLower(name)
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]*entry{}
	}
	e, ok := c.entries[key]
	if ok && (e.png == nil || time.Now().Before(e.until)) {
		c.mu.Unlock()
		// Another request may still be fetching this one.
		select {
		case <-e.ready:
			return e.png
		case <-ctx.Done():
			return Placeholder
		}
	}
	// If the cached head is out of date, keep its picture in case the refresh fails.
	var stale []byte
	if ok && !bytes.Equal(e.png, Placeholder) {
		stale = e.png
	}
	if len(c.entries) >= maxEntries {
		c.prune()
	}
	e = &entry{ready: make(chan struct{})}
	c.entries[key] = e
	c.mu.Unlock()

	// Fetch without the request's context so a closed tab doesn't
	// leave a failure cached for everyone else.
	fctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	img, err := c.lookup(fctx, name)
	c.mu.Lock()
	now := time.Now()
	switch {
	case err == nil && now.Sub(c.joined[key]) < rejoinWindow:
		e.png, e.until = img, now.Add(rejoinKeep)
	case err == nil:
		e.png, e.until = img, now.Add(keepFor)
	case stale != nil:
		e.png, e.until = stale, now.Add(retryStale)
	default:
		e.png, e.until = Placeholder, now.Add(retryAfter)
	}
	c.mu.Unlock()
	close(e.ready)
	return e.png
}

// Forget marks a player's head as out of date, so the next Get asks Mojang
// again. Call it when a player joins a server: a changed skin then shows up
// straight away. The old picture stays in use if the refresh fails.
func (c *Cache) Forget(name string) {
	if !validName.MatchString(name) {
		return
	}
	key := strings.ToLower(name)
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.joined == nil {
		c.joined = map[string]time.Time{}
	}
	for k, t := range c.joined {
		if now.Sub(t) >= rejoinWindow {
			delete(c.joined, k)
		}
	}
	c.joined[key] = now
	// An entry still being fetched has no picture yet and is left alone.
	if e, ok := c.entries[key]; ok && e.png != nil {
		e.until = time.Time{}
	}
}

// prune drops expired entries, or everything if none have expired.
// The caller holds c.mu.
func (c *Cache) prune() {
	now := time.Now()
	for k, e := range c.entries {
		if e.png != nil && now.After(e.until) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) >= maxEntries {
		for k, e := range c.entries {
			if e.png != nil {
				delete(c.entries, k)
			}
		}
	}
}

// lookup goes name → UUID → skin URL → skin → head.
func (c *Cache) lookup(ctx context.Context, name string) ([]byte, error) {
	var prof struct {
		ID string `json:"id"`
	}
	if err := c.getJSON(ctx, "https://api.mojang.com/users/profiles/minecraft/"+name, &prof); err != nil {
		return nil, err
	}
	if len(prof.ID) != 32 {
		return nil, errors.New("no such player")
	}
	var sess struct {
		Properties []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"properties"`
	}
	if err := c.getJSON(ctx, "https://sessionserver.mojang.com/session/minecraft/profile/"+prof.ID, &sess); err != nil {
		return nil, err
	}
	skinURL := ""
	for _, p := range sess.Properties {
		if p.Name != "textures" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(p.Value)
		if err != nil {
			return nil, err
		}
		var tex struct {
			Textures struct {
				Skin struct {
					URL string `json:"url"`
				} `json:"SKIN"`
			} `json:"textures"`
		}
		if err := json.Unmarshal(raw, &tex); err != nil {
			return nil, err
		}
		skinURL = tex.Textures.Skin.URL
	}
	// Only follow links to Mojang's own texture host.
	const host = "textures.minecraft.net/texture/"
	switch {
	case strings.HasPrefix(skinURL, "http://"+host):
		skinURL = "https://" + strings.TrimPrefix(skinURL, "http://")
	case strings.HasPrefix(skinURL, "https://"+host):
	default:
		return nil, errors.New("player has no custom skin")
	}
	skin, err := c.Fetch(ctx, skinURL)
	if err != nil {
		return nil, err
	}
	return Head(skin)
}

func (c *Cache) getJSON(ctx context.Context, url string, v any) error {
	b, err := c.Fetch(ctx, url)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Head cuts the face and hat layer out of a skin PNG and scales it to Size×Size.
func Head(skin []byte) ([]byte, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(skin))
	if err != nil {
		return nil, err
	}
	// Skins are 64×64, or 64×32 in the old format.
	if cfg.Width != 64 || (cfg.Height != 64 && cfg.Height != 32) {
		return nil, fmt.Errorf("unexpected skin size %d×%d", cfg.Width, cfg.Height)
	}
	src, err := png.Decode(bytes.NewReader(skin))
	if err != nil {
		return nil, err
	}
	face := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	draw.Draw(face, face.Bounds(), src, image.Pt(8, 8), draw.Src)
	if hasHat(src) {
		draw.Draw(face, face.Bounds(), src, image.Pt(40, 8), draw.Over)
	}
	return encode(face), nil
}

// hasHat reports whether the hat area is meant to be drawn. Old skins often
// fill it with solid colour; like the game, ignore it unless some of it is
// transparent.
func hasHat(src image.Image) bool {
	for y := range 16 {
		for x := 32; x < 64; x++ {
			if _, _, _, a := src.At(x, y).RGBA(); a < 0x8000 {
				return true
			}
		}
	}
	return false
}

// encode scales an 8×8 face up to Size×Size without blurring and returns PNG bytes.
func encode(face image.Image) []byte {
	const k = Size / 8
	out := image.NewNRGBA(image.Rect(0, 0, Size, Size))
	for y := range Size {
		for x := range Size {
			c := color.NRGBAModel.Convert(face.At(x/k, y/k)).(color.NRGBA)
			c.A = 255 // a head is always solid
			out.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, out)
	return b.Bytes()
}

// placeholder draws a plain grey face.
func placeholder() []byte {
	face := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	skin := color.NRGBA{0x9a, 0xa1, 0xab, 255}
	dark := color.NRGBA{0x5b, 0x62, 0x6c, 255}
	draw.Draw(face, face.Bounds(), image.NewUniform(skin), image.Point{}, draw.Src)
	for x := range 8 {
		face.SetNRGBA(x, 0, dark) // hair
		face.SetNRGBA(x, 1, dark)
	}
	face.SetNRGBA(1, 4, dark) // eyes
	face.SetNRGBA(2, 4, dark)
	face.SetNRGBA(5, 4, dark)
	face.SetNRGBA(6, 4, dark)
	face.SetNRGBA(3, 6, dark) // mouth
	face.SetNRGBA(4, 6, dark)
	return encode(face)
}

var client = &http.Client{Timeout: 15 * time.Second}

func get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", install.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxSkin))
}
