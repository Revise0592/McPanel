// Package icon turns an uploaded picture into a Minecraft server icon.
package icon

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
)

// Size is the width and height Minecraft requires for server-icon.png.
const Size = 64

// FileName is the icon file in a Java server's folder.
const FileName = "server-icon.png"

// maxSide guards against huge images that would use a lot of memory to decode.
const maxSide = 8192

// Make reads a PNG, JPEG or GIF, crops it to a centred square and scales it
// to Size×Size. It returns the result as PNG bytes.
func Make(r io.Reader) ([]byte, error) { return MakeSize(r, Size) }

// MakeSize is Make with a chosen output size.
func MakeSize(r io.Reader, n int) ([]byte, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, errors.New("the file isn't a PNG, JPEG or GIF picture")
	}
	if cfg.Width > maxSide || cfg.Height > maxSide {
		return nil, errors.New("the picture is too large; use one under 8192×8192")
	}
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, errors.New("the picture couldn't be read")
	}
	var out bytes.Buffer
	if err := png.Encode(&out, scale(src, square(src), n)); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// square returns the largest centred square of img.
func square(img image.Image) image.Rectangle {
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	x := b.Min.X + (b.Dx()-side)/2
	y := b.Min.Y + (b.Dy()-side)/2
	return image.Rect(x, y, x+side, y+side)
}

// scale resizes the square area r of src to n×n. Each output pixel is the
// average of the source pixels it covers, so shrinking stays smooth and small
// pixel-art pictures are enlarged without blurring.
func scale(src image.Image, r image.Rectangle, n int) image.Image {
	dst := image.NewNRGBA(image.Rect(0, 0, n, n))
	side := r.Dx()
	for dy := range n {
		y0 := r.Min.Y + dy*side/n
		y1 := max(r.Min.Y+(dy+1)*side/n, y0+1)
		for dx := range n {
			x0 := r.Min.X + dx*side/n
			x1 := max(r.Min.X+(dx+1)*side/n, x0+1)
			// Sum premultiplied colour so transparent pixels don't darken edges.
			var sr, sg, sb, sa, count uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					c := color.RGBA64Model.Convert(src.At(x, y)).(color.RGBA64)
					sr += uint64(c.R)
					sg += uint64(c.G)
					sb += uint64(c.B)
					sa += uint64(c.A)
					count++
				}
			}
			dst.Set(dx, dy, color.RGBA64{
				R: uint16(sr / count), G: uint16(sg / count),
				B: uint16(sb / count), A: uint16(sa / count),
			})
		}
	}
	return dst
}
