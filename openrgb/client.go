package openrgb

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const TCPAddr = "127.0.0.1:6742"

const SocketName = "openrgb.sock"

func DefaultAddr() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		p := filepath.Join(dir, SocketName)
		if fi, err := os.Stat(p); err == nil && fi.Mode()&os.ModeSocket != 0 {
			return "unix:" + p
		}
	}
	return TCPAddr
}

const maxProtocol = 5

const magic = "ORGB"

const (
	pktControllerCount   = 0
	pktControllerData    = 1
	pktProtocolVersion   = 40
	pktSetClientName     = 50
	pktDeviceListUpdated = 100
	pktResizeZone        = 1000
	pktUpdateLEDs        = 1050
	pktUpdateZoneLEDs    = 1051
	pktUpdateSingleLED   = 1052
	pktSetCustomMode     = 1100
	pktUpdateMode        = 1101
	pktSaveMode          = 1102
)

const (
	ModeFlagHasSpeed             = 1 << 0
	ModeFlagHasDirectionLR       = 1 << 1
	ModeFlagHasDirectionUD       = 1 << 2
	ModeFlagHasDirectionHV       = 1 << 3
	ModeFlagHasBrightness        = 1 << 4
	ModeFlagHasPerLEDColor       = 1 << 5
	ModeFlagHasModeSpecificColor = 1 << 6
	ModeFlagHasRandomColor       = 1 << 7
)

const (
	ColorModeNone = iota
	ColorModePerLED
	ColorModeModeSpecific
	ColorModeRandom
)

var deviceTypes = []string{
	"Motherboard", "DRAM", "GPU", "Cooler", "LED Strip", "Keyboard", "Mouse",
	"Mousemat", "Headset", "Headset Stand", "Gamepad", "Light", "Speaker",
	"Virtual", "Storage", "Case", "Microphone", "Accessory", "Keypad",
}

type Mode struct {
	Index                        int
	Name                         string
	Value                        int32
	Flags                        uint32
	SpeedMin, SpeedMax           uint32
	BrightnessMin, BrightnessMax uint32
	ColorsMin, ColorsMax         uint32
	Speed                        uint32
	Brightness                   uint32
	Direction                    uint32
	ColorMode                    uint32
	Colors                       []Color
}

type Segment struct {
	Name     string
	Type     int32
	StartIdx uint32
	LEDCount uint32
}

type Zone struct {
	Name                      string
	Type                      int32
	LEDsMin, LEDsMax          uint32
	LEDCount                  uint32
	MatrixHeight, MatrixWidth uint32
	Matrix                    []uint32
	Segments                  []Segment
	Flags                     uint32
}

type LED struct {
	Name  string
	Value uint32
}

type Device struct {
	Index       int
	Type        uint32
	Name        string
	Vendor      string
	Description string
	Version     string
	Serial      string
	Location    string
	ActiveMode  int32
	Modes       []Mode
	Zones       []Zone
	LEDs        []LED
	Colors      []Color
}

func (d *Device) TypeName() string {
	if int(d.Type) < len(deviceTypes) {
		return deviceTypes[d.Type]
	}
	return "Unknown"
}

func (d *Device) DirectMode() (Mode, bool) {
	for _, want := range []string{"direct", "static", "custom"} {
		for _, m := range d.Modes {
			if strings.EqualFold(m.Name, want) {
				return m, true
			}
		}
	}
	return Mode{}, false
}

type Client struct {
	conn     net.Conn
	protocol uint32
}

func Dial(addr, name string) (*Client, error) {
	if addr == "" {
		addr = DefaultAddr()
	}
	network, address := "tcp", addr
	if p, ok := strings.CutPrefix(addr, "unix:"); ok {
		network, address = "unix", p
	}
	conn, err := net.DialTimeout(network, address, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w (is the openrgb server running?)", addr, err)
	}
	c := &Client{conn: conn}

	var w writer
	w.u32(maxProtocol)
	if err := c.send(0, pktProtocolVersion, w.b); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if data, err := c.recv(pktProtocolVersion); err == nil && len(data) >= 4 {
		if server := binary.LittleEndian.Uint32(data); server < maxProtocol {
			c.protocol = server
		} else {
			c.protocol = maxProtocol
		}
	}
	_ = conn.SetReadDeadline(time.Time{})

	if err := c.send(0, pktSetClientName, append([]byte(name), 0)); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) Protocol() uint32 { return c.protocol }

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) send(dev, pkt uint32, data []byte) error {
	var h [16]byte
	copy(h[0:4], magic)
	binary.LittleEndian.PutUint32(h[4:8], dev)
	binary.LittleEndian.PutUint32(h[8:12], pkt)
	binary.LittleEndian.PutUint32(h[12:16], uint32(len(data)))
	if _, err := c.conn.Write(h[:]); err != nil {
		return fmt.Errorf("write header (packet %d): %w", pkt, err)
	}
	if len(data) > 0 {
		if _, err := c.conn.Write(data); err != nil {
			return fmt.Errorf("write body (packet %d): %w", pkt, err)
		}
	}
	return nil
}

func (c *Client) recv(want uint32) ([]byte, error) {
	for {
		var h [16]byte
		if _, err := io.ReadFull(c.conn, h[:]); err != nil {
			return nil, fmt.Errorf("read header: %w", err)
		}
		if string(h[0:4]) != magic {
			return nil, fmt.Errorf("bad magic %q, stream is out of sync", h[0:4])
		}
		pkt := binary.LittleEndian.Uint32(h[8:12])
		data := make([]byte, binary.LittleEndian.Uint32(h[12:16]))
		if _, err := io.ReadFull(c.conn, data); err != nil {
			return nil, fmt.Errorf("read body (packet %d): %w", pkt, err)
		}
		switch pkt {
		case want:
			return data, nil
		case pktDeviceListUpdated:
			continue
		default:
			return nil, fmt.Errorf("unexpected packet %d, want %d", pkt, want)
		}
	}
}

func (c *Client) DeviceCount() (int, error) {
	if err := c.send(0, pktControllerCount, nil); err != nil {
		return 0, err
	}
	data, err := c.recv(pktControllerCount)
	if err != nil {
		return 0, err
	}
	if len(data) < 4 {
		return 0, fmt.Errorf("short controller count reply (%d bytes)", len(data))
	}
	return int(binary.LittleEndian.Uint32(data)), nil
}

func (c *Client) Device(idx int) (*Device, error) {
	var w writer
	if c.protocol >= 1 {
		w.u32(c.protocol)
	}
	if err := c.send(uint32(idx), pktControllerData, w.b); err != nil {
		return nil, err
	}
	data, err := c.recv(pktControllerData)
	if err != nil {
		return nil, err
	}
	return c.parseDevice(idx, data)
}

func (c *Client) Devices() ([]*Device, error) {
	n, err := c.DeviceCount()
	if err != nil {
		return nil, err
	}
	out := make([]*Device, 0, n)
	for i := range n {
		d, err := c.Device(i)
		if err != nil {
			return nil, fmt.Errorf("device %d: %w", i, err)
		}
		out = append(out, d)
	}
	return out, nil
}

func (c *Client) parseDevice(idx int, data []byte) (*Device, error) {
	r := &reader{b: data}
	r.u32()
	d := &Device{Index: idx, Type: r.u32(), Name: r.str()}
	if c.protocol >= 1 {
		d.Vendor = r.str()
	}
	d.Description, d.Version, d.Serial, d.Location = r.str(), r.str(), r.str(), r.str()

	numModes := int(r.u16())
	d.ActiveMode = r.i32()
	d.Modes = make([]Mode, 0, numModes)
	for i := range numModes {
		m := Mode{Index: i, Name: r.str(), Value: r.i32(), Flags: r.u32()}
		m.SpeedMin, m.SpeedMax = r.u32(), r.u32()
		if c.protocol >= 3 {
			m.BrightnessMin, m.BrightnessMax = r.u32(), r.u32()
		}
		m.ColorsMin, m.ColorsMax = r.u32(), r.u32()
		m.Speed = r.u32()
		if c.protocol >= 3 {
			m.Brightness = r.u32()
		}
		m.Direction, m.ColorMode = r.u32(), r.u32()
		m.Colors = make([]Color, r.u16())
		for j := range m.Colors {
			m.Colors[j] = r.color()
		}
		d.Modes = append(d.Modes, m)
	}

	d.Zones = make([]Zone, r.u16())
	for i := range d.Zones {
		z := Zone{Name: r.str(), Type: r.i32()}
		z.LEDsMin, z.LEDsMax, z.LEDCount = r.u32(), r.u32(), r.u32()
		if matrixLen := r.u16(); matrixLen > 0 {
			z.MatrixHeight, z.MatrixWidth = r.u32(), r.u32()
			z.Matrix = make([]uint32, z.MatrixHeight*z.MatrixWidth)
			for j := range z.Matrix {
				z.Matrix[j] = r.u32()
			}
		}
		if c.protocol >= 4 {
			z.Segments = make([]Segment, r.u16())
			for j := range z.Segments {
				z.Segments[j] = Segment{Name: r.str(), Type: r.i32(), StartIdx: r.u32(), LEDCount: r.u32()}
			}
		}
		if c.protocol >= 5 {
			z.Flags = r.u32()
		}
		d.Zones[i] = z
	}

	d.LEDs = make([]LED, r.u16())
	for i := range d.LEDs {
		d.LEDs[i] = LED{Name: r.str(), Value: r.u32()}
	}
	d.Colors = make([]Color, r.u16())
	for i := range d.Colors {
		d.Colors[i] = r.color()
	}

	if r.err != nil {
		return nil, fmt.Errorf("parse controller %d: %w", idx, r.err)
	}
	return d, nil
}

func (c *Client) SetMode(dev int, m Mode) error {
	var w writer
	w.u32(0)
	w.i32(int32(m.Index))
	w.str(m.Name)
	w.i32(m.Value)
	w.u32(m.Flags)
	w.u32(m.SpeedMin)
	w.u32(m.SpeedMax)
	if c.protocol >= 3 {
		w.u32(m.BrightnessMin)
		w.u32(m.BrightnessMax)
	}
	w.u32(m.ColorsMin)
	w.u32(m.ColorsMax)
	w.u32(m.Speed)
	if c.protocol >= 3 {
		w.u32(m.Brightness)
	}
	w.u32(m.Direction)
	w.u32(m.ColorMode)
	w.u16(uint16(len(m.Colors)))
	for _, col := range m.Colors {
		w.color(col)
	}
	return c.send(uint32(dev), pktUpdateMode, w.sized())
}

func (c *Client) UpdateLEDs(dev int, colors []Color) error {
	var w writer
	w.u32(0)
	w.u16(uint16(len(colors)))
	for _, col := range colors {
		w.color(col)
	}
	return c.send(uint32(dev), pktUpdateLEDs, w.sized())
}

func (c *Client) UpdateZoneLEDs(dev, zone int, colors []Color) error {
	var w writer
	w.u32(0)
	w.u32(uint32(zone))
	w.u16(uint16(len(colors)))
	for _, col := range colors {
		w.color(col)
	}
	return c.send(uint32(dev), pktUpdateZoneLEDs, w.sized())
}

func (c *Client) SetColor(d *Device, col Color) error {
	if m, ok := d.DirectMode(); ok {
		if m.ColorMode == ColorModeModeSpecific {
			n := max(int(m.ColorsMin), 1)
			m.Colors = make([]Color, n)
			for i := range m.Colors {
				m.Colors[i] = col
			}
		}
		if err := c.SetMode(d.Index, m); err != nil {
			return fmt.Errorf("%s: set mode %q: %w", d.Name, m.Name, err)
		}
		if m.ColorMode == ColorModeModeSpecific {
			return nil
		}
	}
	colors := make([]Color, len(d.LEDs))
	for i := range colors {
		colors[i] = col
	}
	if err := c.UpdateLEDs(d.Index, colors); err != nil {
		return fmt.Errorf("%s: update leds: %w", d.Name, err)
	}
	return nil
}

func (c *Client) ResizeZone(dev, zone, size int) error {
	var w writer
	w.i32(int32(zone))
	w.i32(int32(size))
	return c.send(uint32(dev), pktResizeZone, w.b)
}
