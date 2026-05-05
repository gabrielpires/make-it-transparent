package transparent

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestParseHex(t *testing.T) {
	cases := []struct {
		in      string
		want    color.NRGBA
		wantErr bool
	}{
		{"#ff0000", color.NRGBA{R: 255, A: 255}, false},
		{"ff0000", color.NRGBA{R: 255, A: 255}, false},
		{"#FFF", color.NRGBA{R: 255, G: 255, B: 255, A: 255}, false},
		{"  #00ff00  ", color.NRGBA{G: 255, A: 255}, false},
		{"#1234", color.NRGBA{}, true},
		{"#xyzxyz", color.NRGBA{}, true},
		{"", color.NRGBA{}, true},
	}
	for _, tc := range cases {
		got, err := ParseHex(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseHex(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
			continue
		}
		if err == nil && got != tc.want {
			t.Errorf("ParseHex(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestOptionsValidate(t *testing.T) {
	mk := func(tol int) Options {
		return Options{Targets: []Target{{Color: color.NRGBA{R: 255, A: 255}, Tolerance: tol}}}
	}
	if err := mk(-1).Validate(); err == nil {
		t.Error("expected error for negative tolerance")
	}
	if err := mk(MaxTolerance + 1).Validate(); err == nil {
		t.Error("expected error for too-large tolerance")
	}
	if err := (Options{}).Validate(); err == nil {
		t.Error("expected error for empty targets")
	}
	if err := mk(0).Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestApply_ExactMatch(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255}) // red — should go transparent
	src.SetNRGBA(1, 0, color.NRGBA{R: 254, A: 255}) // close but not exact
	src.SetNRGBA(0, 1, color.NRGBA{G: 255, A: 255}) // green — keep
	src.SetNRGBA(1, 1, color.NRGBA{B: 255, A: 200}) // blue — keep
	out, err := Apply(src, Options{Targets: []Target{{Color: color.NRGBA{R: 255, A: 255}}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.NRGBAAt(0, 0).A; got != 0 {
		t.Errorf("red pixel alpha = %d, want 0", got)
	}
	if got := out.NRGBAAt(1, 0).A; got != 255 {
		t.Errorf("near-red pixel alpha = %d, want 255 with tol=0", got)
	}
	if got := out.NRGBAAt(0, 1).A; got != 255 {
		t.Errorf("green pixel alpha = %d, want 255", got)
	}
	if got := out.NRGBAAt(1, 1).A; got != 200 {
		t.Errorf("blue pixel alpha = %d, want 200 (preserved)", got)
	}
}

func TestApply_Tolerance(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 3))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	src.SetNRGBA(0, 1, color.NRGBA{R: 250, G: 5, B: 5, A: 255}) // dist ≈ sqrt(25+25+25)=8.6
	src.SetNRGBA(0, 2, color.NRGBA{R: 200, A: 255})             // dist = 55
	out, err := Apply(src, Options{Targets: []Target{{Color: color.NRGBA{R: 255, A: 255}, Tolerance: 10}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.NRGBAAt(0, 0).A != 0 || out.NRGBAAt(0, 1).A != 0 {
		t.Error("expected first two pixels transparent within tolerance")
	}
	if out.NRGBAAt(0, 2).A == 0 {
		t.Error("third pixel should NOT be transparent (out of tolerance)")
	}
}

func TestApply_PreservesNonTargetExactly(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 12, G: 34, B: 56, A: 78})
	out, _ := Apply(src, Options{Targets: []Target{{Color: color.NRGBA{R: 255, A: 255}}}})
	got := out.NRGBAAt(0, 0)
	want := color.NRGBA{R: 12, G: 34, B: 56, A: 78}
	if got != want {
		t.Errorf("non-target pixel modified: got %+v want %+v", got, want)
	}
}

func TestApply_NilSource(t *testing.T) {
	if _, err := Apply(nil, Options{}); err == nil {
		t.Error("expected error for nil src")
	}
}

func TestApply_FromRGBASource(t *testing.T) {
	// Confirm draw.Draw normalisation works for non-NRGBA inputs.
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.Set(0, 0, color.RGBA{R: 255, A: 255})
	out, err := Apply(src, Options{Targets: []Target{{Color: color.NRGBA{R: 255, A: 255}}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.NRGBAAt(0, 0).A != 0 {
		t.Error("expected red pixel from RGBA source to become transparent")
	}
}

func TestApply_MultipleTargets(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})             // red
	src.SetNRGBA(1, 0, color.NRGBA{B: 255, A: 255})             // blue
	src.SetNRGBA(2, 0, color.NRGBA{G: 255, A: 255})             // green — kept
	src.SetNRGBA(3, 0, color.NRGBA{R: 10, G: 10, B: 10, A: 99}) // dark — kept
	out, err := Apply(src, Options{
		Targets: []Target{
			{Color: color.NRGBA{R: 255, A: 255}},
			{Color: color.NRGBA{B: 255, A: 255}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.NRGBAAt(0, 0).A != 0 {
		t.Error("red should be transparent")
	}
	if out.NRGBAAt(1, 0).A != 0 {
		t.Error("blue should be transparent")
	}
	if out.NRGBAAt(2, 0).A != 255 {
		t.Error("green should be preserved")
	}
	if out.NRGBAAt(3, 0).A != 99 {
		t.Error("dark pixel alpha should be preserved")
	}
}

func TestApply_PerTargetTolerance(t *testing.T) {
	// Two targets with very different tolerances; pixels are picked so that
	// each one is only matched by its own target's tolerance.
	src := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})             // exact red
	src.SetNRGBA(1, 0, color.NRGBA{R: 200, A: 255})             // 55 from red — only matches if red.tol >= 55
	src.SetNRGBA(2, 0, color.NRGBA{B: 255, A: 255})             // exact blue
	src.SetNRGBA(3, 0, color.NRGBA{B: 250, G: 5, R: 5, A: 255}) // ~8.7 from blue — matches with tol 10
	out, err := Apply(src, Options{
		Targets: []Target{
			{Color: color.NRGBA{R: 255, A: 255}, Tolerance: 0},  // exact only
			{Color: color.NRGBA{B: 255, A: 255}, Tolerance: 10}, // forgiving
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.NRGBAAt(0, 0).A != 0 {
		t.Error("exact red should match red target")
	}
	if out.NRGBAAt(1, 0).A == 0 {
		t.Error("near-red should NOT match (red.tol=0)")
	}
	if out.NRGBAAt(2, 0).A != 0 {
		t.Error("exact blue should match")
	}
	if out.NRGBAAt(3, 0).A != 0 {
		t.Error("near-blue should match (blue.tol=10)")
	}
}

func FuzzParseHex(f *testing.F) {
	for _, s := range []string{"#ffffff", "fff", "  #abc  ", "#000000", "", "#xyz"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		// Must never panic.
		_, _ = ParseHex(s)
	})
}

func BenchmarkApply_4MP(b *testing.B) {
	const w, h = 2000, 2000
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	// Half target colour, half not — exercises both branches.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				src.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
			} else {
				src.SetNRGBA(x, y, color.NRGBA{B: 255, A: 255})
			}
		}
	}
	opts := Options{Targets: []Target{{Color: color.NRGBA{R: 255, A: 255}, Tolerance: 10}}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Apply(src, opts); err != nil {
			b.Fatal(err)
		}
	}
}

func TestEncodePNG_RoundTrip(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 0})
	src.SetNRGBA(1, 0, color.NRGBA{G: 128, A: 255})

	var buf bytes.Buffer
	if err := EncodePNG(&buf, src); err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := png.Decode(&buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Bounds() != src.Bounds() {
		t.Errorf("bounds mismatch: %v vs %v", decoded.Bounds(), src.Bounds())
	}
	r, g, b, a := decoded.At(0, 0).RGBA()
	if a != 0 {
		t.Errorf("expected transparent first pixel, got rgba=%d,%d,%d,%d", r, g, b, a)
	}
}
