// Command gen-og produces web/og.png — a 1200x630 OpenGraph image.
// Run with: go run ./cmd/gen-og
//
// We deliberately keep the renderer dependency-free. No text — the brand mark
// is a gradient triangle layered over a transparency-checker rectangle, which
// is the same shape used in the favicon, scaled up.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

const (
	W = 1200
	H = 630
)

func main() {
	img := image.NewNRGBA(image.Rect(0, 0, W, H))

	// Background: subtle vertical gradient, near-black.
	for y := 0; y < H; y++ {
		t := float64(y) / float64(H)
		r := uint8(10 + t*6)
		g := uint8(10 + t*6)
		b := uint8(15 + t*12)
		row := color.NRGBA{R: r, G: g, B: b, A: 255}
		for x := 0; x < W; x++ {
			img.SetNRGBA(x, y, row)
		}
	}

	// Centered brand mark: a 360x360 checker square overlaid by an indigo→fuchsia
	// triangle on its top-right half (matches favicon).
	const size = 380
	cx := (W - size) / 2
	cy := (H - size) / 2
	cellSize := 24
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Rounded corners — skip 24px radius.
			if !insideRounded(x, y, size, size, 28) {
				continue
			}
			// Checker base.
			cellX := x / cellSize
			cellY := y / cellSize
			var c color.NRGBA
			if (cellX+cellY)%2 == 0 {
				c = color.NRGBA{R: 0xfa, G: 0xfa, B: 0xfa, A: 255}
			} else {
				c = color.NRGBA{R: 0xd4, G: 0xd4, B: 0xd8, A: 255}
			}
			// Triangle overlay: keep upper-right half (x+y < size means lower-left).
			if x+y > size {
				// gradient over the triangle (indigo→fuchsia diagonally)
				t := float64(x+y-size) / float64(size)
				if t > 1 {
					t = 1
				}
				c = lerp(color.NRGBA{R: 0x63, G: 0x66, B: 0xf1, A: 255},
					color.NRGBA{R: 0xd9, G: 0x46, B: 0xef, A: 255}, t)
			}
			img.SetNRGBA(cx+x, cy+y, c)
		}
	}

	out, err := os.Create("web/og.png")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := png.Encode(out, img); err != nil {
		_ = out.Close()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := out.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote web/og.png")
}

func lerp(a, b color.NRGBA, t float64) color.NRGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return color.NRGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: 255,
	}
}

func insideRounded(x, y, w, h, radius int) bool {
	if x < radius && y < radius {
		dx, dy := x-radius, y-radius
		return dx*dx+dy*dy <= radius*radius
	}
	if x >= w-radius && y < radius {
		dx, dy := x-(w-radius-1), y-radius
		return dx*dx+dy*dy <= radius*radius
	}
	if x < radius && y >= h-radius {
		dx, dy := x-radius, y-(h-radius-1)
		return dx*dx+dy*dy <= radius*radius
	}
	if x >= w-radius && y >= h-radius {
		dx, dy := x-(w-radius-1), y-(h-radius-1)
		return dx*dx+dy*dy <= radius*radius
	}
	return true
}
