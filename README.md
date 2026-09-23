# rgb — PC lighting from Go

A small Go client for the [OpenRGB](https://openrgb.org) SDK protocol. OpenRGB is
the Linux stand-in for SignalRGB: it owns the per-vendor USB/HID/SMBus quirks and
exposes everything over a socket. This repo speaks that protocol directly — no
cgo, no dependencies.

```sh
./rgb list
./rgb effect comet
```

## Commands

| | |
|---|---|
| `rgb list` | devices, zones, modes, LED counts |
| `rgb effect comet` | comets in the shell palette, Ctrl-C to stop |
| `rgb effect list` | the effect and the current palette |
| `rgb set <colour> [-d N\|-name S]` | paint everything, or a selection |
| `rgb off [-d N\|-name S]` | blackout |
| `rgb rainbow [-speed S] [-fps F]` | hue cycle, ignores the palette |
| `rgb resize -d N -z Z -n C` | declare LED count on an ARGB header |

Colours are `#RRGGBB`, `RRGGBB`, or a name (`red`, `cyan`, `purple`, `orange`, …).
### Prefer `-name` over `-d`

Indices are assigned in *detection order* and shift whenever a bus appears.
Enabling i2c inserted the two DRAM modules at 0 and 1, pushing the motherboard
from 0 to 2 and the mouse from 1 to 3 — every hardcoded `-d` silently retargeted.
`-name` matches device name or type and survives that:

```sh
./rgb set '#FF6600' -name dram
./rgb set cyan      -name mouse
./rgb set '#8A2BE2' -name asus
```

## The comet effect

`rgb effect comet` throws comets down each device in the shell's own colours:
a `primary` head trailing an `accent` tail over the `background`. It reads
`~/.config/tabularium-imperium/colors.json` — the file the shell's theme manager
renders from `templates/blueshell-colors.json`, holding exactly `background`,
`primary` and `accent`.

```sh
rgb effect comet              # until Ctrl-C
rgb effect comet -speed 2     # twice the pace
rgb effect list               # the effect, plus the palette in force
```

Nothing about a comet is fixed. Every launch draws its own:

| | |
|---|---|
| direction | either way along the device, 50/50 |
| rate | 0.30–0.95 device lengths per second |
| tail | 18–50% of the device, floored at 1.5 LEDs |
| brightness | 70–100% of the way up the ramp, so some run dimmer |
| wait until the next | `0.12s + Exp(0.9s)` |

The wait is exponential, which gives Poisson arrivals — clusters and lulls
rather than an evenly spaced procession. Comets overlap freely; where two meet,
the brighter wins rather than summing, which would blow past `primary` and
flatten both tails.

Rates and tails are fractions of each device's *own* length, so a 40-LED header
and a 4-LED mouse take about the same time to cross and read as the same effect.
Each device also gets its own generator, so they drift apart instead of firing
in unison. A comet enters from just off the end, so it arrives tail-last rather
than appearing whole — which is why the strip is briefly dark at startup.

Rendered as ASCII, `@` head through `.` tail:

```
 1.20s |=+*#%@:--==+**#%%@                      | 2 in flight
 1.52s |::--==*##%@    .:---==+*##%@@           | 2 in flight
 1.92s |      ::--=+*#%%@           .::--===+*#%| 2 in flight
 2.32s |           .:--==+*#%%@                 | 1 in flight
```

### Following theme changes

`Colors.qml` watches `colors.json` with `watchChanges: true`, so the shell
re-themes live. The effect does the same, picking up a new palette on the next
frame — no restart, no flag.

### Autostart

The NixOS module starts it — `rgb-comet.service`, a `systemd --user` unit
wanted by `default.target`, so it comes up with your session:

```
systemd --user
  ├── openrgb.service     PrivateNetwork, socket-only
  └── rgb-comet.service   BindsTo=openrgb.service
        └── rgb effect comet
```

`BindsTo` ties the effect's life to the server: without one there is nothing to
drive, and restarting the server invalidates the connection either way.

`openrgb.service` is `Type=simple`, so systemd calls it started the moment its
script execs — before the socket exists. An `ExecStartPre` therefore waits for
the socket (bounded at 15s) instead of letting the effect fail and restart-loop
through boot.

```sh
systemctl --user status rgb-comet
systemctl --user restart rgb-comet
systemctl --user stop rgb-comet      # to drive the lights by hand instead
```

The shell's template set also carries a `post_hook` field (`defaultTemplateSet`
in `src/internal/backend/theme.go`) if you ever want something else to fire on a
theme change — the effect already follows `colors.json` by itself.

### Colour handling

Every level goes through one function, `ramp`, mapping `0 → background`,
`0.5 → accent`, `1 → primary`, so the effect can never show a colour the shell
did not choose. There is a test for exactly that.

Blending runs in linear light rather than raw sRGB bytes. An LED's output is
roughly proportional to duty cycle while perception is not, so a byte-wise lerp
spends most of its range in the bright half: tails would look washed out and
step visibly near their ends. Mid-grey comes out at 186, not 128.

`background` is `#000000` in the current theme, so it really is "off" — which is
what gives the comets their contrast. Under a theme with a lit background (the
blue one is `#0040a1`) they read as a travelling wash instead.

## Transport: a Unix socket, not a port

OpenRGB 1.0 speaks its SDK **only over TCP** — `--server-host` / `--server-port`
are the sole options and there is no `AF_UNIX` code in the binary. That is
upstream's constraint, not a choice here.

It matters because the SDK protocol has **no authentication of any kind**, and a
loopback port is reachable by every local uid. `127.0.0.1:6742` is not a privacy
boundary: any process on the machine can enumerate and drive your hardware.

So the NixOS module runs the server with `PrivateNetwork=true`. The unit gets its
own loopback, the TCP port exists only inside it, and a `socat` bridge in the
same unit publishes a Unix socket at `$XDG_RUNTIME_DIR/openrgb.sock`, mode `0600`,
in a directory that is already `0700`. That socket is the only way in.

`DefaultAddr()` prefers that socket when it exists and falls back to loopback TCP,
so the client needs no flag either way. `-addr` takes both forms:

```sh
./rgb list                                   # auto
./rgb list -addr unix:/run/user/1000/openrgb.sock
./rgb list -addr 127.0.0.1:6742
```

### Why both halves share one unit

`JoinsNamespaceOf=` does **not** share a network namespace between two
`systemd --user` units. Configured that way, the bridge reports
`PrivateNetwork=yes` and `JoinsNamespaceOf=` in `systemctl show`, yet its
`/proc/PID/ns/net` is the *host* namespace — it silently reaches the host's port
instead of the isolated one. Measured, not assumed: with the backend stopped, a
query through the socket still returned the host server's devices.

`PrivateNetwork` itself works fine in user units, including socket-activated
ones. Only the cross-unit sharing fails. Hence one unit running both processes.

Network isolation does not touch USB, hidraw or i2c, so detection still finds
everything — verified inside the namespace, all four devices present.

## This machine

| # | Device | How | LEDs |
|---|---|---|---|
| 0,1 | ENE DRAM ×2 | SMBus `/dev/i2c-13` @ `0x71`, `0x73` | 8 each |
| 2 | ASUS ROG STRIX B850-I (AURA) | HID `/dev/hidraw9` | 2 ARGB headers |
| 3 | SteelSeries Aerox 9 Wireless | HID `/dev/hidraw3` | 4 zones |

### ARGB headers need a length

Addressable strips carry no "how long am I" signal, so OpenRGB reports **0 LEDs**
on a fresh header and nothing lights up. Tell it:

```sh
./rgb resize -d 2 -z 0 -n 40
./rgb resize -d 2 -z 1 -n 40
```

Over-declaring is harmless (surplus pixels go nowhere); under-declaring leaves the
tail of the chain dark. The size persists in the config of whichever server you
talked to — a root server stores it under `/var/lib/OpenRGB`, yours under
`~/.config/OpenRGB`, and switching between them looks like the resize was lost.

### The GPU has no OpenRGB support

The card is a PowerColor RX 9070 XT — PCI `1002:7550`, subsystem `148c:2435`.
Its i2c buses *are* exposed (`AMDGPU SMU 0/1`, `AMDGPU DM i2c hw bus 0-3`) and
OpenRGB scans them during "Detecting I2C PCI devices", but finds no controller:
OpenRGB matches GPUs by PCI subsystem ID against per-board drivers, and there is
no entry for this Navi 48 board. Nothing to configure — it needs upstream support.

## NixOS

`nix/nixosModule.nix`, exported from `flake.nix` as `nixosModules.default`.

It deliberately avoids `services.hardware.openrgb.enable`, which would run the
server as a **root** systemd unit. OpenRGB ships its device rules as
`TAG+="uaccess"`, telling logind to ACL the device to whoever holds the active
seat — root gains nothing from those rules, and a dedicated system account would
be actively *excluded* by them. Both USB devices here are covered that way
(`0b05:19af` Aura, `1038:185a` Aerox 9). So the module installs the package and
its udev rules and runs the server as a `systemd --user` unit instead.

Checked by evaluating the module inside a throwaway NixOS config:

| | |
|---|---|
| root `systemd.services.openrgb` | absent |
| `services.hardware.openrgb.enable` | `false` |
| server `PrivateNetwork` | `true` |
| server ExecStart | `openrgb-socket %t/openrgb.sock` |
| comet ExecStart | `…rgb-0.1.0/bin/rgb effect comet` |
| comet `BindsTo` / `After` | `openrgb.service` |
| `ConditionUser` (both) | `christoph` |
| `environment.systemPackages` | includes `rgb-0.1.0` |
| `boot.kernelModules` | `i2c-dev`, `i2c-piix4` |

`/dev/i2c-*` gets both a uaccess ACL *and* `group i2c` from `hardware.i2c.enable`,
and the module puts you in that group — so DRAM stays reachable even if the
server starts before the seat ACLs land.

After `nixos-rebuild switch`:

```sh
systemctl --user status openrgb rgb-comet
ls -l $XDG_RUNTIME_DIR/openrgb.sock
rgb effect list                  # rgb is on $PATH now
```

Stop any hand-started `rgb effect comet` first, or two of them will fight over
the same LEDs.

Zone sizes are already in `~/.config/OpenRGB/Configuration.json` (`leds_count: 40`
on both headers), so they carry over — no need to re-run `rgb resize`.

If DRAM ever stops being detected, AMD SMBus access can be blocked by ACPI
arbitration; add `boot.kernelParams = [ "acpi_enforce_resources=lax" ]`.

## Protocol notes

`openrgb/wire.go` and `openrgb/client.go` implement the SDK wire format:

- 16-byte header: magic `ORGB`, device index, packet ID, body length — all LE.
- Colours are `uint32` laid out `0x00BBGGRR`, i.e. bytes R, G, B, 0.
- Strings are a `uint16` length *including* the NUL, then the bytes.
- The controller-description layout is **version-gated**: vendor appears at
  protocol ≥ 1, mode brightness at ≥ 3, zone segments at ≥ 4, zone flags at ≥ 5.
  `Dial` negotiates down to whatever the server supports, and parsing follows.
- To control colour you first switch the device into `Direct` mode
  (`UPDATE_MODE`, 1101), then stream frames with `UPDATE_LEDS` (1050). Modes whose
  colour model is *mode-specific* (typically `Static`) instead carry the colour
  inside the mode packet; `SetColor` handles both.
- `RESIZEZONE` (1000) takes a bare `zone, size` pair with no length prefix,
  unlike most packets.

## Building

The module installs `rgb` system-wide, so it is on `$PATH` after a rebuild.
Standalone:

```sh
nix build .#rgb        # -> ./result/bin/rgb
nix run .#rgb -- list
```

`nix/package.nix` is a `buildGoModule` with `vendorHash = null`: nothing
third-party is imported, so there is no `go.sum` and nothing to vendor. Its
source is a `lib.fileset` of `go.mod` plus the `.go` files, so editing this
README does not rebuild the binary, and `go test ./...` runs in the sandbox as
part of the build. The Go module is named after the repo, so `postInstall`
renames the binary to `rgb`.

The module calls `package.nix` directly rather than going through an overlay,
which keeps it usable on its own.

For a quick loop without nix:

```sh
CGO_ENABLED=0 go build -o rgb .
```

`CGO_ENABLED=0` avoids a dynamic link against a glibc store path that isn't
resolvable in every shell here.
