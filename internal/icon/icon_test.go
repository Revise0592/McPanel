package icon

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func encode(t *testing.T, img image.Image) []byte {
	t.Helper()
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

func TestMakeCropsAndScales(t *testing.T) {
	// 300×100: red middle square with blue on both sides. The crop keeps only red.
	src := image.NewRGBA(image.Rect(0, 0, 300, 100))
	for y := range 100 {
		for x := range 300 {
			c := color.RGBA{0, 0, 255, 255}
			if x >= 100 && x < 200 {
				c = color.RGBA{255, 0, 0, 255}
			}
			src.Set(x, y, c)
		}
	}
	out, err := Make(bytes.NewReader(encode(t, src)))
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, out)
	if b := img.Bounds(); b.Dx() != Size || b.Dy() != Size {
		t.Fatalf("size = %v", b)
	}
	for _, p := range []image.Point{{0, 0}, {63, 63}, {32, 32}} {
		r, g, b, _ := img.At(p.X, p.Y).RGBA()
		if r>>8 != 255 || g != 0 || b != 0 {
			t.Errorf("pixel %v = %d,%d,%d; want red", p, r>>8, g>>8, b>>8)
		}
	}
}

func TestMakeEnlargesSmallPicture(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	src.Set(0, 0, color.RGBA{0, 255, 0, 255})
	out, err := Make(bytes.NewReader(encode(t, src)))
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, out)
	// Each source pixel becomes a sharp 4×4 block.
	if _, g, _, _ := img.At(3, 3).RGBA(); g>>8 != 255 {
		t.Error("top-left block should be green")
	}
	if _, g, _, a := img.At(4, 4).RGBA(); g != 0 || a != 0 {
		t.Error("next block should be transparent")
	}
}

func TestMakeAcceptsJPEG(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 128, 128)), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Make(&b); err != nil {
		t.Fatal(err)
	}
}

func TestMakeRejectsNonImage(t *testing.T) {
	if _, err := Make(strings.NewReader("not a picture")); err == nil {
		t.Fatal("want error")
	}
}
