package openrgb

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Color struct{ R, G, B uint8 }

func (c Color) String() string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }

func (c Color) wire() uint32 { return uint32(c.R) | uint32(c.G)<<8 | uint32(c.B)<<16 }

func colorFromWire(v uint32) Color {
	return Color{R: uint8(v), G: uint8(v >> 8), B: uint8(v >> 16)}
}

var named = map[string]Color{
	"off":     {0, 0, 0},
	"black":   {0, 0, 0},
	"white":   {255, 255, 255},
	"red":     {255, 0, 0},
	"green":   {0, 255, 0},
	"blue":    {0, 0, 255},
	"yellow":  {255, 255, 0},
	"cyan":    {0, 255, 255},
	"magenta": {255, 0, 255},
	"purple":  {128, 0, 255},
	"orange":  {255, 64, 0},
	"pink":    {255, 32, 128},
}

func ParseColor(s string) (Color, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if c, ok := named[s]; ok {
		return c, nil
	}
	h := strings.TrimPrefix(s, "#")
	if len(h) != 6 {
		return Color{}, fmt.Errorf("bad colour %q: want #RRGGBB or a name", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return Color{}, fmt.Errorf("bad colour %q: %w", s, err)
	}
	return Color{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}, nil
}

func HSV(h, s, v float64) Color {
	h = h - 360*float64(int(h/360))
	if h < 0 {
		h += 360
	}
	c := v * s
	x := c * (1 - abs(mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return Color{R: uint8((r + m) * 255), G: uint8((g + m) * 255), B: uint8((b + m) * 255)}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func mod(a, b float64) float64 { return a - b*float64(int(a/b)) }

type reader struct {
	b   []byte
	off int
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if r.off+n > len(r.b) {
		r.err = fmt.Errorf("truncated packet: need %d bytes at offset %d, have %d", n, r.off, len(r.b))
		return nil
	}
	s := r.b[r.off : r.off+n]
	r.off += n
	return s
}

func (r *reader) u16() uint16 {
	s := r.take(2)
	if s == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(s)
}

func (r *reader) u32() uint32 {
	s := r.take(4)
	if s == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(s)
}

func (r *reader) i32() int32 { return int32(r.u32()) }

func (r *reader) str() string {
	n := r.u16()
	if n == 0 {
		return ""
	}
	s := r.take(int(n))
	if s == nil {
		return ""
	}
	return string(s[:n-1])
}

func (r *reader) color() Color { return colorFromWire(r.u32()) }

type writer struct{ b []byte }

func (w *writer) u16(v uint16) { w.b = binary.LittleEndian.AppendUint16(w.b, v) }
func (w *writer) u32(v uint32) { w.b = binary.LittleEndian.AppendUint32(w.b, v) }
func (w *writer) i32(v int32)  { w.u32(uint32(v)) }

func (w *writer) str(s string) {
	w.u16(uint16(len(s) + 1))
	w.b = append(w.b, s...)
	w.b = append(w.b, 0)
}

func (w *writer) color(c Color) { w.u32(c.wire()) }

func (w *writer) sized() []byte {
	binary.LittleEndian.PutUint32(w.b[0:4], uint32(len(w.b)))
	return w.b
}

func Blend(a, b Color, t float64) Color {
	switch {
	case t <= 0:
		return a
	case t >= 1:
		return b
	}
	mix := func(x, y uint8) uint8 {
		return fromLinear(toLinear(x)*(1-t) + toLinear(y)*t)
	}
	return Color{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B)}
}

func Scale(c Color, f float64) Color { return Blend(Color{}, c, f) }

const gamma = 2.2

func toLinear(v uint8) float64 { return math.Pow(float64(v)/255, gamma) }

func fromLinear(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(math.Round(math.Pow(v, 1/gamma) * 255))
}
