package theme

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"luminarium-mechanicus/openrgb"
)

type Palette struct {
	Background openrgb.Color
	Primary    openrgb.Color
	Accent     openrgb.Color
}

func DefaultPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "colors.json"
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "tabularium-imperium", "colors.json")
}

type paletteFile struct {
	Background string `json:"background"`
	Primary    string `json:"primary"`
	Accent     string `json:"accent"`
}

func Load(path string) (Palette, error) {
	var p Palette
	b, err := os.ReadFile(path)
	if err != nil {
		return p, fmt.Errorf("read palette: %w", err)
	}
	var f paletteFile
	if err := json.Unmarshal(b, &f); err != nil {
		return p, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, field := range []struct {
		name string
		raw  string
		dst  *openrgb.Color
	}{
		{"background", f.Background, &p.Background},
		{"primary", f.Primary, &p.Primary},
		{"accent", f.Accent, &p.Accent},
	} {
		if field.raw == "" {
			return p, fmt.Errorf("%s: missing %q", path, field.name)
		}
		c, err := openrgb.ParseColor(field.raw)
		if err != nil {
			return p, fmt.Errorf("%s: %s: %w", path, field.name, err)
		}
		*field.dst = c
	}
	return p, nil
}

func (p Palette) String() string {
	return fmt.Sprintf("bg %s  primary %s  accent %s", p.Background, p.Primary, p.Accent)
}

func Watch(path string, interval time.Duration) (<-chan Palette, func()) {
	out := make(chan Palette)
	stop := make(chan struct{})

	go func() {
		defer close(out)
		var lastMod time.Time
		var lastSize int64
		if fi, err := os.Stat(path); err == nil {
			lastMod, lastSize = fi.ModTime(), fi.Size()
		}
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				fi, err := os.Stat(path)
				if err != nil || (fi.ModTime().Equal(lastMod) && fi.Size() == lastSize) {
					continue
				}
				lastMod, lastSize = fi.ModTime(), fi.Size()
				p, err := Load(path)
				if err != nil {
					continue
				}
				select {
				case out <- p:
				case <-stop:
					return
				}
			}
		}
	}()

	var once bool
	return out, func() {
		if !once {
			once = true
			close(stop)
		}
	}
}
