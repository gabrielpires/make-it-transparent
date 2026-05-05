// Package handler exposes the HTTP handler that drives the transparency
// pipeline: accept multipart upload, decode, mutate, encode PNG.
package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"  // register decoders
	_ "image/jpeg" //
	_ "image/png"  //
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gabrielpires/make-it-transparent/internal/transparent"
)

// Limits constrains request size and parsed memory.
type Limits struct {
	// MaxUploadBytes is enforced via http.MaxBytesReader.
	MaxUploadBytes int64
	// MaxParseMemoryBytes is the multipart in-memory threshold; the rest spills
	// to a temp file.
	MaxParseMemoryBytes int64
}

// DefaultLimits is a sensible 25 MB cap for an in-memory image tool.
func DefaultLimits() Limits {
	return Limits{
		MaxUploadBytes:      25 << 20,
		MaxParseMemoryBytes: 10 << 20,
	}
}

// MaxColors caps the number of distinct target colors per request to keep
// the inner loop bounded.
const MaxColors = 16

// DefaultTolerance is used when a color row omits its tolerance field.
const DefaultTolerance = 10

// allowedFormats restricts which image formats we will decode. If a future
// edit accidentally registers another decoder via blank import (e.g.
// _ "golang.org/x/image/webp"), this whitelist keeps the attack surface fixed.
// Keys are the names returned by image.DecodeConfig.
var allowedFormats = map[string]struct{}{
	"png":  {},
	"jpeg": {},
	"gif":  {},
}

// Transparent returns an HTTP handler that performs the colour-to-alpha pass.
//
// Form fields:
//
//	image      — the image file (PNG/JPEG/GIF accepted, PNG returned)
//	color      — one or more hex strings, repeated. e.g. "#ff00aa".
//	             Comma-separated values within a single field are also accepted.
//	tolerance  — repeated, paired by index with `color`. 0..442. If fewer
//	             tolerances than colors are sent, missing entries default to 10.
func Transparent(limits Limits) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "POST required")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, limits.MaxUploadBytes)
		if err := r.ParseMultipartForm(limits.MaxParseMemoryBytes); err != nil {
			code := http.StatusBadRequest
			// MaxBytesReader signals overflow with this exact prefix.
			if strings.Contains(err.Error(), "http: request body too large") {
				code = http.StatusRequestEntityTooLarge
			}
			writeError(w, code, fmt.Sprintf("invalid multipart upload: %v", err))
			return
		}

		targets, err := parseTargets(r.Form["color"], r.Form["tolerance"])
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		file, header, err := r.FormFile("image")
		if err != nil {
			writeError(w, http.StatusBadRequest, "missing 'image' file field")
			return
		}
		defer file.Close()

		// Buffer the upload so we can DecodeConfig (peek dimensions) before
		// committing memory to a full decode — this is the decompression-bomb
		// guard. The buffer size is already capped by MaxBytesReader.
		raw, err := io.ReadAll(file)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("read upload: %v", err))
			return
		}

		cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot read image header: %v", err))
			return
		}
		if _, ok := allowedFormats[format]; !ok {
			writeError(w, http.StatusUnsupportedMediaType,
				fmt.Sprintf("unsupported image format %q (allowed: PNG, JPEG, GIF)", format))
			return
		}
		if cfg.Width <= 0 || cfg.Height <= 0 {
			writeError(w, http.StatusBadRequest, "image has invalid dimensions")
			return
		}
		if int64(cfg.Width)*int64(cfg.Height) > int64(transparent.MaxPixels) {
			writeError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("image %dx%d exceeds %d-pixel limit", cfg.Width, cfg.Height, transparent.MaxPixels))
			return
		}

		src, _, err := image.Decode(bytes.NewReader(raw))
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot decode image: %v", err))
			return
		}

		out, err := transparent.Apply(src, transparent.Options{Targets: targets})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, downloadName(header.Filename)))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_ = transparent.EncodePNG(w, out)
	})
}

// parseTargets pairs repeated `color` form fields with parallel `tolerance`
// fields by index. Comma-separated values within a single color field are
// expanded inline; tolerance fields are NOT split (each is its own value).
// If there are more colors than tolerances, missing entries default to
// DefaultTolerance.
func parseTargets(colorRaw, tolRaw []string) ([]transparent.Target, error) {
	colors := make([]string, 0, len(colorRaw))
	for _, group := range colorRaw {
		for _, hex := range strings.Split(group, ",") {
			hex = strings.TrimSpace(hex)
			if hex != "" {
				colors = append(colors, hex)
			}
		}
	}
	if len(colors) == 0 {
		return nil, fmt.Errorf("at least one 'color' field required")
	}
	if len(colors) > MaxColors {
		return nil, fmt.Errorf("too many colors (max %d)", MaxColors)
	}
	if len(tolRaw) > len(colors) {
		return nil, fmt.Errorf("more tolerance values (%d) than colors (%d)", len(tolRaw), len(colors))
	}

	out := make([]transparent.Target, 0, len(colors))
	for i, hex := range colors {
		c, err := transparent.ParseHex(hex)
		if err != nil {
			return nil, err
		}
		tol := DefaultTolerance
		if i < len(tolRaw) && strings.TrimSpace(tolRaw[i]) != "" {
			n, err := strconv.Atoi(strings.TrimSpace(tolRaw[i]))
			if err != nil {
				return nil, fmt.Errorf("invalid tolerance %q: %w", tolRaw[i], err)
			}
			tol = n
		}
		out = append(out, transparent.Target{Color: c, Tolerance: tol})
	}
	return out, nil
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// downloadName turns a user-supplied filename into a safe download name. We:
//   - strip any directory components (filepath.Base, then any forward-slash too,
//     defensively, since browsers may send paths with their native separator)
//   - strip ASCII control characters and quotes
//   - cap the length
//   - drop the original extension (output is always PNG)
//
// The returned value is suffixed with "-transparent.png".
func downloadName(original string) string {
	base := filepath.Base(original)
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	if i := strings.LastIndexByte(base, '\\'); i >= 0 {
		base = base[i+1:]
	}
	// Strip extension.
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	// Strip control chars and quotes.
	var b strings.Builder
	for _, r := range base {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\\' {
			continue
		}
		b.WriteRune(r)
	}
	clean := strings.TrimSpace(b.String())
	if clean == "" || clean == "." || clean == ".." {
		clean = "image"
	}
	if len(clean) > 80 {
		clean = clean[:80]
	}
	return clean + "-transparent.png"
}
