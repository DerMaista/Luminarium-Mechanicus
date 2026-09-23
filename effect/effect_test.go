package effect

import (
	"math/rand"
	"testing"

	"luminarium-mechanicus/openrgb"
	"luminarium-mechanicus/theme"
)

var pal = theme.Palette{
	Background: openrgb.Color{R: 0, G: 0, B: 0},
	Primary:    openrgb.Color{R: 0, G: 255, B: 0},
	Accent:     openrgb.Color{R: 0, G: 0x79, B: 0},
}

func seeded(leds int, seed int64) *comet {
	return newComet(leds, rand.New(rand.NewSource(seed)))
}

func TestRampHitsPaletteExactly(t *testing.T) {
	for _, tc := range []struct {
		x    float64
		want openrgb.Color
	}{
		{0, pal.Background},
		{0.5, pal.Accent},
		{1, pal.Primary},
	} {
		if got := ramp(pal, tc.x); got != tc.want {
			t.Errorf("ramp(%v) = %v, want %v", tc.x, got, tc.want)
		}
	}
}

func TestRampClampsOutOfRange(t *testing.T) {
	if got := ramp(pal, -5); got != pal.Background {
		t.Errorf("ramp(-5) = %v, want background", got)
	}
	if got := ramp(pal, 5); got != pal.Primary {
		t.Errorf("ramp(5) = %v, want primary", got)
	}
}

func TestRampIsMonotonic(t *testing.T) {
	prev := -1.0
	for i := 0; i <= 100; i++ {
		c := ramp(pal, float64(i)/100)
		lum := float64(c.R) + float64(c.G) + float64(c.B)
		if lum < prev-0.5 {
			t.Fatalf("brightness dipped at x=%v: %v after %v", float64(i)/100, lum, prev)
		}
		prev = lum
	}
}

func TestBlendIsGammaAware(t *testing.T) {
	mid := openrgb.Blend(openrgb.Color{}, openrgb.Color{R: 255, G: 255, B: 255}, 0.5)
	if mid.G <= 128 {
		t.Errorf("mid grey = %v, want brighter than the naive 128", mid.G)
	}
}

func TestBlendEndpoints(t *testing.T) {
	a := openrgb.Color{R: 10, G: 20, B: 30}
	b := openrgb.Color{R: 200, G: 100, B: 50}
	if got := openrgb.Blend(a, b, 0); got != a {
		t.Errorf("t=0 gave %v, want %v", got, a)
	}
	if got := openrgb.Blend(a, b, 1); got != b {
		t.Errorf("t=1 gave %v, want %v", got, b)
	}
}

func TestStaysInPalette(t *testing.T) {
	e := seeded(24, 7)
	buf := make([]openrgb.Color, 24)
	for step := range 400 {
		e.Render(pal, buf, float64(step)*0.05)
		for i, c := range buf {
			if c.R != 0 || c.B != 0 {
				t.Fatalf("led %d leaked outside the palette at step %d: %v", i, step, c)
			}
		}
	}
}

func TestLaunchesAreVaried(t *testing.T) {
	e := seeded(40, 3)
	buf := make([]openrgb.Color, 40)
	seen := map[float64]bool{}
	rates := map[float64]bool{}
	tails := map[float64]bool{}
	peaks := map[float64]bool{}
	for step := range 600 {
		e.Render(pal, buf, float64(step)*0.05)
		for _, r := range e.runs {
			seen[r.dir] = true
			rates[r.rate] = true
			tails[r.tail] = true
			peaks[r.peak] = true
		}
	}
	if len(seen) < 2 {
		t.Errorf("only one direction ever used: %v", seen)
	}
	for name, m := range map[string]int{"rates": len(rates), "tails": len(tails), "peaks": len(peaks)} {
		if m < 5 {
			t.Errorf("%s barely varied: %d distinct values", name, m)
		}
	}
}

func TestGapsAreIrregular(t *testing.T) {
	e := seeded(40, 11)
	gaps := map[float64]bool{}
	for range 200 {
		gaps[e.gap()] = true
	}
	if len(gaps) < 100 {
		t.Errorf("gaps barely varied: %d distinct of 200", len(gaps))
	}
}

func TestDevicesDesync(t *testing.T) {
	a, b := seeded(40, 100), seeded(40, 200)
	bufA := make([]openrgb.Color, 40)
	bufB := make([]openrgb.Color, 40)
	same := 0
	const steps = 200
	for step := range steps {
		at := float64(step) * 0.05
		a.Render(pal, bufA, at)
		b.Render(pal, bufB, at)
		if equal(bufA, bufB) {
			same++
		}
	}
	if same > steps/4 {
		t.Errorf("devices tracked each other on %d of %d frames", same, steps)
	}
}

func equal(a, b []openrgb.Color) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFinishedCometsArePruned(t *testing.T) {
	e := seeded(40, 5)
	buf := make([]openrgb.Color, 40)
	for step := range 2000 {
		e.Render(pal, buf, float64(step)*0.05)
	}
	if len(e.runs) > 16 {
		t.Errorf("%d comets still in flight after a long run; pruning is not working", len(e.runs))
	}
}

func TestClockJumpIsBounded(t *testing.T) {
	e := seeded(40, 9)
	buf := make([]openrgb.Color, 40)
	e.Render(pal, buf, 0)
	e.Render(pal, buf, 1e6)
	if len(e.runs) > cometMaxCatchUp {
		t.Errorf("clock jump produced %d comets", len(e.runs))
	}
}

func TestShortDevices(t *testing.T) {
	for _, n := range []int{1, 2, 4} {
		e := seeded(n, 42)
		buf := make([]openrgb.Color, n)
		lit := false
		for step := range 200 {
			e.Render(pal, buf, float64(step)*0.05)
			for _, c := range buf {
				if c != pal.Background {
					lit = true
				}
			}
		}
		if !lit {
			t.Errorf("%d-led device never lit up", n)
		}
	}
}

func TestOnlyCometIsRegistered(t *testing.T) {
	all := All()
	if len(all) != 1 || all[0].Name != "comet" {
		t.Errorf("registry = %v, want just comet", all)
	}
	if _, ok := Lookup("comet"); !ok {
		t.Error("comet not found by Lookup")
	}
}

func TestHeadTravels(t *testing.T) {
	e := seeded(40, 1)
	buf := make([]openrgb.Color, 40)
	var heads []int
	for step := range 200 {
		e.Render(pal, buf, float64(step)*0.05)
		best, bestLum := -1, 0.0
		for i, c := range buf {
			if lum := float64(c.G); lum > bestLum {
				best, bestLum = i, lum
			}
		}
		if best >= 0 {
			heads = append(heads, best)
		}
	}
	if len(heads) < 20 {
		t.Fatalf("strip was lit on only %d frames", len(heads))
	}
	distinct := map[int]bool{}
	for _, h := range heads {
		distinct[h] = true
	}
	if len(distinct) < 5 {
		t.Errorf("head barely moved: %d distinct positions", len(distinct))
	}
}
