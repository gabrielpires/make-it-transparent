// Package transparent makes a target color in an image transparent via
// per-pixel alpha replacement. It is intentionally simple: pixels whose RGB
// distance from the target falls within the tolerance get alpha=0; everything
// else is preserved unchanged.
package transparent

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"strconv"
	"strings"
)

// MaxTolerance is the largest meaningful Euclidean RGB distance:
// sqrt(3 * 255^2) ≈ 441.67.
const MaxTolerance = 442

// MaxPixels caps the decoded image size to keep memory bounded. 16 megapixels
// → ~64 MiB for an NRGBA buffer; we hold up to two such buffers per request.
const MaxPixels = 16_000_000

// Target is a single color-to-knock-out together with its own tolerance.
type Target struct {
	// Color is the RGB to match. Alpha is ignored.
	Color color.NRGBA
	// Tolerance is the max Euclidean RGB distance from Color that still counts
	// as a match (0 = exact, MaxTolerance ≈ matches everything).
	Tolerance int
}

// Validate checks the tolerance range.
func (t Target) Validate() error {
	if t.Tolerance < 0 || t.Tolerance > MaxTolerance {
		return fmt.Errorf("tolerance %d out of range [0,%d]", t.Tolerance, MaxTolerance)
	}
	return nil
}

// Options configures a transparency pass.
type Options struct {
	// Targets are the (color, tolerance) pairs to apply. A pixel is matched if
	// it falls within ANY target's individual tolerance.
	Targets []Target
}

// Validate ensures options are usable.
func (o Options) Validate() error {
	if len(o.Targets) == 0 {
		return errors.New("at least one target required")
	}
	for i, t := range o.Targets {
		if err := t.Validate(); err != nil {
			return fmt.Errorf("target %d: %w", i, err)
		}
	}
	return nil
}

// Apply returns a new NRGBA image with pixels matching any of opts.Targets
// (each with its own tolerance) replaced by fully transparent pixels.
func Apply(src image.Image, opts Options) (*image.NRGBA, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errors.New("nil source image")
	}

	bounds := src.Bounds()
	out := image.NewNRGBA(bounds)
	// draw.Src copies pixels straight in (no alpha blending), normalising
	// whatever colour model src uses into NRGBA so we can iterate Pix directly.
	draw.Draw(out, bounds, src, bounds.Min, draw.Src)

	// Pre-extract target channels + squared tolerance to keep the inner loop tight.
	type tgt struct {
		r, g, b int
		tolSq   int
	}
	targets := make([]tgt, len(opts.Targets))
	for i, t := range opts.Targets {
		targets[i] = tgt{
			r:     int(t.Color.R),
			g:     int(t.Color.G),
			b:     int(t.Color.B),
			tolSq: t.Tolerance * t.Tolerance,
		}
	}

	pix := out.Pix
	for i := 0; i+3 < len(pix); i += 4 {
		pr := int(pix[i+0])
		pg := int(pix[i+1])
		pb := int(pix[i+2])
		for _, t := range targets {
			dr := pr - t.r
			dg := pg - t.g
			db := pb - t.b
			if dr*dr+dg*dg+db*db <= t.tolSq {
				pix[i+3] = 0
				break
			}
		}
	}
	return out, nil
}

// EncodePNG writes img to w as a PNG without DEFLATE compression so the result
// is bit-for-bit a copy of the pixel buffer (still a valid PNG, just larger).
func EncodePNG(w io.Writer, img image.Image) error {
	enc := &png.Encoder{CompressionLevel: png.NoCompression}
	return enc.Encode(w, img)
}

// ParseHex parses CSS-style hex colors: "#RGB", "#RRGGBB", with or without
// the leading '#'. Returns an opaque NRGBA.
func ParseHex(s string) (color.NRGBA, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	switch len(s) {
	case 3:
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	case 6:
		// ok
	default:
		return color.NRGBA{}, fmt.Errorf("hex color must be 3 or 6 digits, got %q", s)
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("invalid hex color %q: %w", s, err)
	}
	return color.NRGBA{
		R: uint8((n >> 16) & 0xff),
		G: uint8((n >> 8) & 0xff),
		B: uint8(n & 0xff),
		A: 255,
	}, nil
}
