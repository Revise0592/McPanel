package heads

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var (
	red  = color.NRGBA{255, 0, 0, 255}
	blue = color.NRGBA{0, 0, 255, 255}
)

// testSkin is a 64×64 skin with a red face and a blue hat pixel at the face's top-left.
func testSkin(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 8; y < 16; y++ {
		for x := 8; x < 16; x++ {
			img.SetNRGBA(x, y, red)
		}
	}
	img.SetNRGBA(40, 8, blue)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func decode(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func same(a, b color.Color) bool {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

func TestHeadCropsFaceAndHat(t *testing.T) {
	out, err := Head(testSkin(t))
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, out)
	if img.Bounds().Dx() != Size || img.Bounds().Dy() != Size {
		t.Fatalf("size %v", img.Bounds())
	}
	if !same(img.At(0, 0), blue) {
		t.Errorf("hat pixel = %v, want blue", img.At(0, 0))
	}
	if !same(img.At(Size-1, Size-1), red) {
		t.Errorf("face pixel = %v, want red", img.At(Size-1, Size-1))
	}
}

func TestHeadRejectsOddSizes(t *testing.T) {
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 10, 10)))
	if _, err := Head(b.Bytes()); err == nil {
		t.Fatal("want error for 10×10 skin")
	}
}

func fakeMojang(t *testing.T, skinURL string, calls *atomic.Int32) Fetcher {
	skin := testSkin(t)
	tex := base64.StdEncoding.EncodeToString([]byte(`{"textures":{"SKIN":{"url":"` + skinURL + `"}}}`))
	return func(ctx context.Context, url string) ([]byte, error) {
		calls.Add(1)
		switch {
		case url == "https://api.mojang.com/users/profiles/minecraft/Alex":
			return []byte(`{"id":"0123456789abcdef0123456789abcdef","name":"Alex"}`), nil
		case strings.HasPrefix(url, "https://sessionserver.mojang.com/"):
			return []byte(`{"properties":[{"name":"textures","value":"` + tex + `"}]}`), nil
		case strings.HasPrefix(url, "https://textures.minecraft.net/"):
			return skin, nil
		}
		return nil, errors.New("not found")
	}
}

func TestGetFetchesOnceAndCaches(t *testing.T) {
	var calls atomic.Int32
	c := &Cache{Fetch: fakeMojang(t, "http://textures.minecraft.net/texture/abc", &calls)}
	first := c.Get(context.Background(), "Alex")
	if bytes.Equal(first, Placeholder) {
		t.Fatal("got placeholder for a known player")
	}
	if !same(decode(t, first).At(0, 0), blue) {
		t.Error("head doesn't come from the skin")
	}
	n := calls.Load()
	if again := c.Get(context.Background(), "alex"); !bytes.Equal(again, first) {
		t.Error("second lookup differs")
	}
	if calls.Load() != n {
		t.Error("second lookup fetched again")
	}
}

func TestGetPlaceholderCases(t *testing.T) {
	var calls atomic.Int32
	c := &Cache{Fetch: fakeMojang(t, "https://evil.example/skin.png", &calls)}
	for _, name := range []string{"Alex", "Nobody", "bad name", "", "../x"} {
		if got := c.Get(context.Background(), name); !bytes.Equal(got, Placeholder) {
			t.Errorf("%q: want placeholder", name)
		}
	}
}

func TestHeadIgnoresSolidHat(t *testing.T) {
	img, _ := png.Decode(bytes.NewReader(testSkin(t)))
	skin := img.(*image.NRGBA)
	for y := range 16 {
		for x := 32; x < 64; x++ {
			skin.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, skin)
	out, err := Head(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !same(decode(t, out).At(0, 0), red) {
		t.Error("solid hat layer covered the face")
	}
}

func TestForgetRefetches(t *testing.T) {
	var calls atomic.Int32
	c := &Cache{Fetch: fakeMojang(t, "https://textures.minecraft.net/texture/abc", &calls)}
	c.Get(context.Background(), "Alex")
	n := calls.Load()
	c.Forget("ALEX")
	if got := c.Get(context.Background(), "Alex"); bytes.Equal(got, Placeholder) {
		t.Fatal("got placeholder after refetch")
	}
	if calls.Load() == n {
		t.Fatal("Forget didn't cause a new lookup")
	}
	// Fetched just after a join, so it's only kept briefly in case Mojang
	// was still handing out the old skin.
	c.mu.Lock()
	until := c.entries["alex"].until
	c.mu.Unlock()
	if left := time.Until(until); left > rejoinKeep {
		t.Errorf("kept for %v after a join, want at most %v", left, rejoinKeep)
	}
}

func TestFailedRefreshKeepsOldHead(t *testing.T) {
	var calls atomic.Int32
	var down atomic.Bool
	ok := fakeMojang(t, "https://textures.minecraft.net/texture/abc", &calls)
	c := &Cache{Fetch: func(ctx context.Context, url string) ([]byte, error) {
		if down.Load() {
			return nil, errors.New("rate limited")
		}
		return ok(ctx, url)
	}}
	first := c.Get(context.Background(), "Alex")
	down.Store(true)
	c.Forget("Alex")
	if got := c.Get(context.Background(), "Alex"); !bytes.Equal(got, first) {
		t.Fatal("a failed refresh replaced the head")
	}
}

func TestForgetIgnoresBadNames(t *testing.T) {
	c := &Cache{}
	c.Forget("bad name")
	c.Forget(".BedrockPlayer")
	if len(c.joined) != 0 {
		t.Errorf("joined = %v, want empty", c.joined)
	}
}
