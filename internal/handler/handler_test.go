package handler

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func makeUpload(t *testing.T, fields map[string]string, fileField, fileName string, fileBody []byte) (*bytes.Buffer, string) {
	t.Helper()
	return makeUploadMulti(t, multiFields(fields), fileField, fileName, fileBody)
}

func multiFields(m map[string]string) map[string][]string {
	out := make(map[string][]string, len(m))
	for k, v := range m {
		out[k] = []string{v}
	}
	return out
}

func makeUploadMulti(t *testing.T, fields map[string][]string, fileField, fileName string, fileBody []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, vs := range fields {
		for _, v := range vs {
			if err := w.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	if fileField != "" {
		fw, err := w.CreateFormFile(fileField, fileName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(fileBody); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, w.FormDataContentType()
}

func makePNG(t *testing.T, w, h int, fill color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestTransparentHandler_HappyPath(t *testing.T) {
	src := makePNG(t, 4, 4, color.NRGBA{R: 255, A: 255})
	body, ct := makeUploadMulti(t,
		map[string][]string{"color": {"#ff0000"}, "tolerance": {"0"}},
		"image", "input.png", src,
	)

	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	Transparent(DefaultLimits()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("content-type=%q", got)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "input-transparent.png") {
		t.Errorf("disposition=%q", cd)
	}
	out, err := png.Decode(rec.Body)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	_, _, _, a := out.At(0, 0).RGBA()
	if a != 0 {
		t.Errorf("expected pixel transparent, got alpha=%d", a)
	}
}

func TestTransparentHandler_RejectsGet(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/transparent", nil)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d", rec.Code)
	}
}

func TestTransparentHandler_BadHex(t *testing.T) {
	body, ct := makeUpload(t,
		map[string]string{"color": "not-a-color"},
		"image", "x.png", makePNG(t, 1, 1, color.NRGBA{A: 255}),
	)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTransparentHandler_BadTolerance(t *testing.T) {
	body, ct := makeUploadMulti(t,
		map[string][]string{"color": {"#000"}, "tolerance": {"9999"}},
		"image", "x.png", makePNG(t, 1, 1, color.NRGBA{A: 255}),
	)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d", rec.Code)
	}
}

func TestTransparentHandler_MissingImage(t *testing.T) {
	body, ct := makeUpload(t,
		map[string]string{"color": "#000"},
		"", "", nil,
	)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d", rec.Code)
	}
}

func TestTransparentHandler_UploadTooLarge(t *testing.T) {
	limits := Limits{MaxUploadBytes: 64, MaxParseMemoryBytes: 32}
	src := makePNG(t, 64, 64, color.NRGBA{R: 255, A: 255}) // > 64 bytes
	body, ct := makeUpload(t, map[string]string{"color": "#000"}, "image", "x.png", src)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(limits).ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTransparentHandler_BogusImageBytes(t *testing.T) {
	body, ct := makeUpload(t,
		map[string]string{"color": "#000"},
		"image", "x.png", []byte("not actually an image"),
	)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d", rec.Code)
	}
}

func TestTransparentHandler_MultipleColors(t *testing.T) {
	// Source: red, green, blue, white in a 2x2.
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	img.SetNRGBA(0, 1, color.NRGBA{B: 255, A: 255})
	img.SetNRGBA(1, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}

	body, ct := makeUploadMulti(t,
		map[string][]string{
			"color":     {"#ff0000", "#0000ff"}, // red + blue
			"tolerance": {"0", "0"},
		},
		"image", "x.png", raw.Bytes(),
	)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	out, err := png.Decode(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := out.At(0, 0).RGBA(); a != 0 {
		t.Error("red should be transparent")
	}
	if _, _, _, a := out.At(0, 1).RGBA(); a != 0 {
		t.Error("blue should be transparent")
	}
	if _, _, _, a := out.At(1, 0).RGBA(); a == 0 {
		t.Error("green should be preserved")
	}
	if _, _, _, a := out.At(1, 1).RGBA(); a == 0 {
		t.Error("white should be preserved")
	}
}

func TestTransparentHandler_PerColorTolerance(t *testing.T) {
	// Pixel A is near-red (dist=55), pixel B is near-blue (dist≈8.7).
	// Red target gets tol=0 (no match), blue target gets tol=10 (matches).
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 200, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{B: 250, G: 5, R: 5, A: 255})
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	body, ct := makeUploadMulti(t,
		map[string][]string{
			"color":     {"#ff0000", "#0000ff"},
			"tolerance": {"0", "10"},
		},
		"image", "x.png", raw.Bytes(),
	)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	out, _ := png.Decode(rec.Body)
	if _, _, _, a := out.At(0, 0).RGBA(); a == 0 {
		t.Error("near-red pixel must NOT be transparent (red tol=0)")
	}
	if _, _, _, a := out.At(1, 0).RGBA(); a != 0 {
		t.Error("near-blue pixel must be transparent (blue tol=10)")
	}
}

func TestTransparentHandler_TooManyTolerances(t *testing.T) {
	src := makePNG(t, 1, 1, color.NRGBA{A: 255})
	body, ct := makeUploadMulti(t,
		map[string][]string{
			"color":     {"#ff0000"},
			"tolerance": {"0", "10"}, // 2 vs 1 colour → reject
		},
		"image", "x.png", src,
	)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTransparentHandler_CommaSeparatedColors(t *testing.T) {
	src := makePNG(t, 2, 1, color.NRGBA{R: 255, A: 255})
	body, ct := makeUploadMulti(t,
		map[string][]string{
			"color":     {"#ff0000, #00ff00"},
			"tolerance": {"0"},
		},
		"image", "x.png", src,
	)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTransparentHandler_DefaultToleranceIsTen(t *testing.T) {
	// One color, no tolerance field => default 10 should match a near-red pixel.
	src := makePNG(t, 1, 1, color.NRGBA{R: 250, G: 5, B: 5, A: 255})
	body, ct := makeUploadMulti(t,
		map[string][]string{"color": {"#ff0000"}},
		"image", "x.png", src,
	)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	out, _ := png.Decode(rec.Body)
	if _, _, _, a := out.At(0, 0).RGBA(); a != 0 {
		t.Error("default tolerance=10 should have made near-red transparent")
	}
}

func TestTransparentHandler_NoColors(t *testing.T) {
	src := makePNG(t, 1, 1, color.NRGBA{A: 255})
	body, ct := makeUploadMulti(t,
		map[string][]string{}, // no color field
		"image", "x.png", src,
	)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d", rec.Code)
	}
}

func TestTransparentHandler_OversizeImage(t *testing.T) {
	// Build a PNG whose width*height exceeds MaxPixels but whose serialized
	// size easily fits under MaxUploadBytes (PNG compresses solid colour well).
	// transparent.MaxPixels is 16M; we go just past that.
	img := image.NewNRGBA(image.Rect(0, 0, 5000, 4000)) // 20M pixels
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	body, ct := makeUpload(t,
		map[string]string{"color": "#000000", "tolerance": "0"},
		"image", "huge.png", raw.Bytes(),
	)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDownloadName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"input.png", "input-transparent.png"},
		{"my image.JPG", "my image-transparent.png"},
		{"/etc/passwd", "passwd-transparent.png"},
		{"..\\..\\windows\\system32\\evil.exe", "evil-transparent.png"},
		{`my"quoted"file.png`, "myquotedfile-transparent.png"}, // quotes stripped
		{"line\r\nbreak.png", "linebreak-transparent.png"},
		{"", "image-transparent.png"},
		{".", "image-transparent.png"},
		{"..", "image-transparent.png"},
		{strings.Repeat("a", 200) + ".png", strings.Repeat("a", 80) + "-transparent.png"},
	}
	for _, tc := range cases {
		got := downloadName(tc.in)
		if got != tc.want {
			t.Errorf("downloadName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseTargets(t *testing.T) {
	t.Run("happy path with explicit tolerances", func(t *testing.T) {
		ts, err := parseTargets([]string{"#ff0000", "#0000ff"}, []string{"0", "20"})
		if err != nil {
			t.Fatal(err)
		}
		if len(ts) != 2 || ts[0].Tolerance != 0 || ts[1].Tolerance != 20 {
			t.Errorf("got %+v", ts)
		}
	})
	t.Run("missing tolerance defaults to 10", func(t *testing.T) {
		ts, err := parseTargets([]string{"#ff0000"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if ts[0].Tolerance != DefaultTolerance {
			t.Errorf("default tolerance = %d", ts[0].Tolerance)
		}
	})
	t.Run("comma-split colors", func(t *testing.T) {
		ts, err := parseTargets([]string{"#ff0000, #00ff00"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(ts) != 2 {
			t.Errorf("got %d targets", len(ts))
		}
	})
	t.Run("rejects too many colors", func(t *testing.T) {
		var many []string
		for i := 0; i < MaxColors+1; i++ {
			many = append(many, "#000000")
		}
		if _, err := parseTargets(many, nil); err == nil {
			t.Error("expected error for too many colors")
		}
	})
	t.Run("rejects bad hex", func(t *testing.T) {
		if _, err := parseTargets([]string{"banana"}, nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("rejects empty", func(t *testing.T) {
		if _, err := parseTargets(nil, nil); err == nil {
			t.Error("expected error")
		}
	})
	t.Run("rejects bad tolerance string", func(t *testing.T) {
		if _, err := parseTargets([]string{"#ff0000"}, []string{"abc"}); err == nil {
			t.Error("expected error")
		}
	})
}

func TestTransparentHandler_RejectsExecutableDisguisedAsImage(t *testing.T) {
	// ELF magic with .png filename — DecodeConfig must fail.
	body, ct := makeUpload(t,
		map[string]string{"color": "#ff0000"},
		"image", "totally-an-image.png",
		[]byte("\x7fELF\x02\x01\x01\x00 ...rest of an ELF binary..."),
	)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTransparentHandler_RejectsHTMLPolyglot(t *testing.T) {
	// HTML with a script tag — wouldn't pass through anyway, but verify it
	// isn't echoed: server returns JSON error, never the bytes.
	payload := []byte(`<html><script>alert(1)</script></html>`)
	body, ct := makeUpload(t,
		map[string]string{"color": "#ff0000"},
		"image", "x.png", payload,
	)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("ct=%q (must be JSON, never image/*)", got)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("<script>")) {
		t.Error("server echoed user-controlled HTML in response body")
	}
}

func TestTransparentHandler_RejectsNonAllowlistedFormat(t *testing.T) {
	// Build a valid BMP — Go's stdlib doesn't decode BMP without an extra
	// import, so this currently fails at DecodeConfig. To prove the format
	// allowlist works even when the decoder is registered, we synthesize a
	// scenario: register a fake "bmp" decoder that DecodeConfig recognises,
	// then confirm the handler refuses it. Since we can't easily monkey-patch
	// image.RegisterFormat from here, we instead test the allowlist directly.
	if _, ok := allowedFormats["bmp"]; ok {
		t.Fatal("'bmp' must NOT be in allowedFormats")
	}
	for _, k := range []string{"png", "jpeg", "gif"} {
		if _, ok := allowedFormats[k]; !ok {
			t.Errorf("%q must be in allowedFormats", k)
		}
	}
}

func TestConcurrency_Limit(t *testing.T) {
	// Wrap a handler that blocks until released; only `max` should be in
	// flight at once.
	const max = 2
	release := make(chan struct{})
	inFlight := make(chan struct{}, 100)
	h := Concurrency(max, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inFlight <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}))

	// Fire (max + 2) requests in parallel.
	results := make(chan int, max+2)
	for i := 0; i < max+2; i++ {
		go func() {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			results <- rec.Code
		}()
	}

	// Wait until `max` are blocked inside the handler.
	for i := 0; i < max; i++ {
		<-inFlight
	}
	// Give the runtime a beat; the next 2 must have been rejected by now.
	// We collect the two non-200 results first.
	rejected := 0
	for i := 0; i < 2; i++ {
		code := <-results
		if code == http.StatusServiceUnavailable {
			rejected++
		}
	}
	if rejected != 2 {
		t.Errorf("got %d rejections, want 2 (concurrency cap not enforced)", rejected)
	}

	// Drain the in-flight ones.
	close(release)
	for i := 0; i < max; i++ {
		<-results
	}
}

// sanity — ensure response body is readable as a stream (no Content-Length/handler quirks).
func TestTransparentHandler_BodyDrains(t *testing.T) {
	src := makePNG(t, 2, 2, color.NRGBA{B: 255, A: 255})
	body, ct := makeUpload(t, map[string]string{"color": "#0000ff"}, "image", "x.png", src)
	req := httptest.NewRequest(http.MethodPost, "/api/transparent", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	Transparent(DefaultLimits()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	n, err := io.Copy(io.Discard, rec.Body)
	if err != nil || n == 0 {
		t.Errorf("read body n=%d err=%v", n, err)
	}
}
