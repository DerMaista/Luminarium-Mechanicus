package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"luminarium-mechanicus/effect"
	"luminarium-mechanicus/openrgb"
	"luminarium-mechanicus/theme"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	cmd, rest := args[0], args[1:]

	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	addr := fs.String("addr", openrgb.DefaultAddr(), "server address: host:port or unix:/path")
	device := fs.Int("d", -1, "device index, or -1 for all")
	name := fs.String("name", "", "select devices whose name or type contains this")
	fps := fs.Int("fps", 30, "frames per second for animated effects")
	speed := fs.Float64("speed", 1, "speed multiplier for animated effects")
	zone := fs.Int("z", -1, "zone index (resize)")
	count := fs.Int("n", -1, "LED count (resize)")
	palette := fs.String("palette", theme.DefaultPath(), "shell colors.json to read")

	switch cmd {
	case "list", "set", "off", "rainbow", "resize", "effect":
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q (try: list, set, off, effect, rainbow, resize)", cmd)
	}

	var arg string
	if cmd == "set" || cmd == "effect" {
		if len(rest) == 0 {
			if cmd == "set" {
				return fmt.Errorf("set needs a colour, e.g. `set red` or `set '#FF00AA'`")
			}
			return fmt.Errorf("effect needs a name; try `rgb effect list`")
		}
		arg, rest = rest[0], rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}

	if cmd == "effect" && arg == "list" {
		listEffects(*palette)
		return nil
	}

	c, err := openrgb.Dial(*addr, "luminarium-mechanicus")
	if err != nil {
		return err
	}
	defer c.Close()

	devices, err := c.Devices()
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return fmt.Errorf("server reports no devices")
	}

	targets, err := selectDevices(devices, *device, *name)
	if err != nil {
		return err
	}

	switch cmd {
	case "list":
		list(c, devices)
		return nil
	case "off":
		return paint(c, targets, openrgb.Color{})
	case "set":
		col, err := openrgb.ParseColor(arg)
		if err != nil {
			return err
		}
		return paint(c, targets, col)
	case "rainbow":
		return rainbow(c, targets, *fps, *speed*40)
	case "effect":
		def, ok := effect.Lookup(arg)
		if !ok {
			return fmt.Errorf("unknown effect %q; try `rgb effect list`", arg)
		}
		return runEffect(c, targets, def, *palette, *fps, *speed)
	case "resize":
		if *zone < 0 || *count < 0 {
			return fmt.Errorf("resize needs -z <zone> -n <led count>")
		}
		if len(targets) != 1 {
			return fmt.Errorf("resize needs exactly one device; got %d (use -d or -name)", len(targets))
		}
		return resize(c, targets[0], *zone, *count)
	}
	return nil
}

func selectDevices(devices []*openrgb.Device, idx int, name string) ([]*openrgb.Device, error) {
	if name != "" {
		if idx >= 0 {
			return nil, fmt.Errorf("use -d or -name, not both")
		}
		needle := strings.ToLower(name)
		var out []*openrgb.Device
		for _, d := range devices {
			if strings.Contains(strings.ToLower(d.Name), needle) ||
				strings.Contains(strings.ToLower(d.TypeName()), needle) {
				out = append(out, d)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("no device matches %q (try `rgb list`)", name)
		}
		return out, nil
	}
	if idx >= 0 {
		if idx >= len(devices) {
			return nil, fmt.Errorf("device %d out of range (%d detected)", idx, len(devices))
		}
		return devices[idx : idx+1], nil
	}
	return devices, nil
}

func usage() {
	fmt.Print(`rgb - drive PC lighting via an OpenRGB SDK server

  rgb list                        detected devices, zones and modes
  rgb set <colour> [-d N|-name S] paint devices one colour
  rgb off [-d N|-name S]          all LEDs black
  rgb effect comet [-speed S]     comets in the shell's colours, Ctrl-C to stop
  rgb effect list                 show the effect and the current palette
  rgb rainbow [-speed S]          hue cycle, ignores the palette
  rgb resize -d N -z Z -n C       set LED count on a resizable ARGB zone

Selection: -d takes a detection index, which shifts whenever hardware or a bus
appears. -name matches device name or type ("dram", "mouse", "asus") and is the
stable choice for scripts.

The comet effect is drawn from the shell's three colours (background, accent,
primary) and re-themes live when colors.json changes.

  -palette PATH   default ~/.config/tabularium-imperium/colors.json
  -fps N          frame rate (default 30)
  -speed F        multiplier on the natural rate (default 1)

Colours: #RRGGBB, RRGGBB, or a name (red, cyan, purple, orange, ...).
Flags:   -addr host:port or unix:/path (default: socket if present, else TCP)
`)
}

func listEffects(palettePath string) {
	fmt.Println("effects (all drawn from background / accent / primary):")
	fmt.Println()
	for _, d := range effect.All() {
		kind := "static "
		if d.Animated {
			kind = "animated"
		}
		fmt.Printf("  %-9s %s  %s\n", d.Name, kind, d.Summary)
	}
	fmt.Println()
	if p, err := theme.Load(palettePath); err == nil {
		fmt.Printf("palette %s\n        %s\n", palettePath, p)
	} else {
		fmt.Printf("palette %s\n        unreadable: %v\n", palettePath, err)
	}
}

func list(c *openrgb.Client, devices []*openrgb.Device) {
	fmt.Printf("connected to OpenRGB, SDK protocol %d, %d device(s)\n\n", c.Protocol(), len(devices))
	for _, d := range devices {
		fmt.Printf("[%d] %s  (%s)\n", d.Index, d.Name, d.TypeName())
		if d.Vendor != "" {
			fmt.Printf("     vendor:   %s\n", d.Vendor)
		}
		if d.Location != "" {
			fmt.Printf("     location: %s\n", d.Location)
		}
		fmt.Printf("     leds:     %d\n", len(d.LEDs))

		for i, z := range d.Zones {
			line := fmt.Sprintf("     zone %d:   %-24s %d led", i, z.Name, z.LEDCount)
			if z.LEDCount != 1 {
				line += "s"
			}
			if z.LEDsMax > z.LEDsMin {
				line += fmt.Sprintf(" (resizable %d-%d)", z.LEDsMin, z.LEDsMax)
			}
			fmt.Println(line)
		}

		names := make([]string, len(d.Modes))
		for i, m := range d.Modes {
			names[i] = m.Name
			if int32(i) == d.ActiveMode {
				names[i] += "*"
			}
		}
		fmt.Printf("     modes:    %s\n", strings.Join(names, ", "))
		if m, ok := d.DirectMode(); ok {
			fmt.Printf("     drive as: %s\n", m.Name)
		} else {
			fmt.Printf("     drive as: (no direct/static mode - colours may not stick)\n")
		}
		fmt.Println()
	}
	fmt.Println("* = currently active mode")
}

func paint(c *openrgb.Client, devices []*openrgb.Device, col openrgb.Color) error {
	for _, d := range devices {
		if err := c.SetColor(d, col); err != nil {
			return err
		}
		note := ""
		if len(d.LEDs) == 0 {
			note = "  (0 leds - needs `rgb resize`)"
		}
		fmt.Printf("[%d] %-42s -> %s%s\n", d.Index, d.Name, col, note)
	}
	return nil
}

func resize(c *openrgb.Client, d *openrgb.Device, zone, count int) error {
	if zone >= len(d.Zones) {
		return fmt.Errorf("zone %d out of range (%s has %d)", zone, d.Name, len(d.Zones))
	}
	z := d.Zones[zone]
	if z.LEDsMax <= z.LEDsMin {
		return fmt.Errorf("zone %q is not resizable", z.Name)
	}
	if uint32(count) < z.LEDsMin || uint32(count) > z.LEDsMax {
		return fmt.Errorf("count %d outside zone %q range %d-%d", count, z.Name, z.LEDsMin, z.LEDsMax)
	}
	if err := c.ResizeZone(d.Index, zone, count); err != nil {
		return err
	}
	updated, err := c.Device(d.Index)
	if err != nil {
		return err
	}
	fmt.Printf("[%d] %s zone %d %q -> %d leds (device total now %d)\n",
		d.Index, d.Name, zone, z.Name, updated.Zones[zone].LEDCount, len(updated.LEDs))
	return nil
}

func slept(from, to time.Time) time.Duration {
	return to.Round(0).Sub(from.Round(0)) - to.Sub(from)
}

const resumeGap = time.Second

func setDirect(c *openrgb.Client, devices []*openrgb.Device) error {
	for _, d := range devices {
		m, ok := d.DirectMode()
		if !ok {
			continue
		}
		if err := c.SetMode(d.Index, m); err != nil {
			return err
		}
	}
	return nil
}

type stage struct {
	dev *openrgb.Device
	eff effect.Effect
	buf []openrgb.Color
}

func runEffect(c *openrgb.Client, devices []*openrgb.Device, def effect.Def, palettePath string, fps int, speed float64) error {
	pal, err := theme.Load(palettePath)
	if err != nil {
		return err
	}
	if fps < 1 {
		fps = 1
	}

	var stages []stage
	for _, d := range devices {
		if len(d.LEDs) == 0 {
			fmt.Fprintf(os.Stderr, "skipping %s: 0 leds, needs `rgb resize`\n", d.Name)
			continue
		}
		stages = append(stages, stage{dev: d, eff: def.New(len(d.LEDs)), buf: make([]openrgb.Color, len(d.LEDs))})
	}
	if len(stages) == 0 {
		return fmt.Errorf("no device has any LEDs to drive")
	}

	driven := make([]*openrgb.Device, len(stages))
	for i, s := range stages {
		driven[i] = s.dev
	}
	if err := setDirect(c, driven); err != nil {
		return err
	}

	draw := func(t float64) error {
		for _, s := range stages {
			s.eff.Render(pal, s.buf, t)
			if err := c.UpdateLEDs(s.dev.Index, s.buf); err != nil {
				return err
			}
		}
		return nil
	}

	if !def.Animated {
		if err := draw(0); err != nil {
			return err
		}
		fmt.Printf("%s across %d device(s)  [%s]\n", def.Name, len(stages), pal)
		return nil
	}

	palCh, stopWatch := theme.Watch(palettePath, 500*time.Millisecond)
	defer stopWatch()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	var frames <-chan time.Time
	if def.Animated {
		tk := time.NewTicker(time.Second / time.Duration(fps))
		defer tk.Stop()
		frames = tk.C
	}

	if err := draw(0); err != nil {
		return err
	}
	fmt.Printf("%s across %d device(s), following %s, Ctrl-C to stop\n", def.Name, len(stages), palettePath)

	start := time.Now()
	last := start
	for {
		select {
		case <-stop:
			fmt.Println("\nstopping")
			return nil
		case p := <-palCh:
			pal = p
			fmt.Printf("palette changed: %s\n", pal)
			if !def.Animated {
				if err := draw(0); err != nil {
					return err
				}
			}
		case <-frames:
			now := time.Now()
			if gap := slept(last, now); gap > resumeGap {
				fmt.Printf("resumed after %s asleep, re-asserting direct mode\n", gap.Round(time.Second))
				if err := setDirect(c, driven); err != nil {
					return err
				}
			}
			last = now
			// Monotonic, so it did not advance while suspended: the comets
			// carry on from where the machine left them rather than jumping.
			if err := draw(now.Sub(start).Seconds() * speed); err != nil {
				return err
			}
		}
	}
}

func rainbow(c *openrgb.Client, devices []*openrgb.Device, fps int, speed float64) error {
	if fps < 1 {
		fps = 1
	}
	if err := setDirect(c, devices); err != nil {
		return err
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	tick := time.NewTicker(time.Second / time.Duration(fps))
	defer tick.Stop()

	fmt.Printf("rainbow across %d device(s) at %d fps, Ctrl-C to stop\n", len(devices), fps)
	start := time.Now()
	last := start
	for {
		select {
		case <-stop:
			fmt.Println("\nstopping")
			return nil
		case <-tick.C:
			now := time.Now()
			if gap := slept(last, now); gap > resumeGap {
				fmt.Printf("resumed after %s asleep, re-asserting direct mode\n", gap.Round(time.Second))
				if err := setDirect(c, devices); err != nil {
					return err
				}
			}
			last = now
			base := now.Sub(start).Seconds() * speed
			for _, d := range devices {
				colors := make([]openrgb.Color, len(d.LEDs))
				for i := range colors {
					spread := 0.0
					if len(colors) > 1 {
						spread = float64(i) / float64(len(colors)) * 360
					}
					colors[i] = openrgb.HSV(base+spread, 1, 1)
				}
				if err := c.UpdateLEDs(d.Index, colors); err != nil {
					return err
				}
			}
		}
	}
}
