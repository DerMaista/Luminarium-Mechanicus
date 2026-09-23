package effect

import (
	"math"
	"math/rand"
	"sort"

	"luminarium-mechanicus/openrgb"
	"luminarium-mechanicus/theme"
)

type Effect interface {
	Render(p theme.Palette, dst []openrgb.Color, t float64)
}

type Def struct {
	Name     string
	Summary  string
	Animated bool
	New      func(leds int) Effect
}

var registry = map[string]Def{}

func register(d Def) { registry[d.Name] = d }

func Lookup(name string) (Def, bool) {
	d, ok := registry[name]
	return d, ok
}

func All() []Def {
	out := make([]Def, 0, len(registry))
	for _, d := range registry {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Animated != out[j].Animated {
			return !out[i].Animated
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func ramp(p theme.Palette, x float64) openrgb.Color {
	switch {
	case x <= 0:
		return p.Background
	case x >= 1:
		return p.Primary
	case x < 0.5:
		return openrgb.Blend(p.Background, p.Accent, x*2)
	default:
		return openrgb.Blend(p.Accent, p.Primary, (x-0.5)*2)
	}
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func init() {
	register(Def{
		Name:     "comet",
		Summary:  "primary heads with accent tails, launched at random over background",
		Animated: true,
		New: func(leds int) Effect {
			return newComet(leds, rand.New(rand.NewSource(rand.Int63())))
		},
	})
}

const (
	cometMinGap  = 0.12
	cometMeanGap = 0.90
	cometMinRate = 0.30
	cometMaxRate = 0.95
	cometMinTail = 0.18
	cometMaxTail = 0.50
	cometMinPeak = 0.70
	cometMaxPeak = 1.00

	cometMaxCatchUp = 64
)

type cometRun struct {
	launch float64
	rate   float64
	tail   float64
	dir    float64
	peak   float64
}

type comet struct {
	n    int
	rng  *rand.Rand
	runs []cometRun
	next float64
}

func newComet(leds int, rng *rand.Rand) *comet {
	return &comet{n: leds, rng: rng}
}

func (c *comet) gap() float64 {
	return cometMinGap + c.rng.ExpFloat64()*cometMeanGap
}

func (c *comet) spawn(at float64) {
	n := float64(c.n)
	dir := 1.0
	if c.rng.Intn(2) == 0 {
		dir = -1
	}
	c.runs = append(c.runs, cometRun{
		launch: at,
		rate:   n * lerp(cometMinRate, cometMaxRate, c.rng.Float64()),
		tail:   math.Max(n*lerp(cometMinTail, cometMaxTail, c.rng.Float64()), 1.5),
		dir:    dir,
		peak:   lerp(cometMinPeak, cometMaxPeak, c.rng.Float64()),
	})
}

func (r cometRun) travel(n float64) float64 { return n + 2*r.tail }

func (r cometRun) head(t, n float64) float64 {
	progress := (t - r.launch) * r.rate
	if r.dir > 0 {
		return -r.tail + progress
	}
	return n + r.tail - progress
}

func (c *comet) Render(p theme.Palette, dst []openrgb.Color, t float64) {
	n := float64(len(dst))

	for i := 0; i < cometMaxCatchUp && c.next <= t; i++ {
		c.spawn(c.next)
		c.next += c.gap()
	}

	live := c.runs[:0]
	for _, r := range c.runs {
		if (t-r.launch)*r.rate <= r.travel(n) {
			live = append(live, r)
		}
	}
	c.runs = live

	for i := range dst {
		level := 0.0
		for _, r := range c.runs {
			d := r.dir * (r.head(t, n) - float64(i))
			if d < 0 || d > r.tail {
				continue
			}
			if l := r.peak * (1 - d/r.tail); l > level {
				level = l
			}
		}
		dst[i] = ramp(p, level)
	}
}
