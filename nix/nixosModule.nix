# RGB lighting: no root daemon, no listening TCP port, comet on login.
#
# Two things shape this module.
#
# 1. No root. services.hardware.openrgb.enable would run the SDK server as a
#    root systemd unit. OpenRGB ships its device rules as TAG+="uaccess", which
#    tells logind to ACL the device to whoever holds the active seat -- root
#    gains nothing from those rules and a system account is *excluded* by them.
#    So the server runs as a systemd --user unit, which is who uaccess names.
#
# 2. No open port. OpenRGB 1.0 speaks its SDK only over TCP (--server-host /
#    --server-port; there is no AF_UNIX support in the binary), and the protocol
#    has no authentication whatsoever. A loopback port is reachable by every
#    local uid, so "127.0.0.1" is not a privacy boundary: any process on the
#    machine could drive or read the hardware. PrivateNetwork gives the unit its
#    own loopback, so that port exists only inside the unit, and a Unix socket
#    in $XDG_RUNTIME_DIR (already mode 0700) becomes the only way in.
#
# The server and the bridge must live in the *same* unit: JoinsNamespaceOf=
# does not share a network namespace between two systemd --user units -- the
# joining unit silently stays in the host namespace.
{ config, lib, pkgs, ... }:

let
  user = "christoph";

  # Loopback port, reachable only inside the server unit's network namespace.
  port = 6742;

  rgb = pkgs.callPackage ./package.nix { };

  runner = pkgs.writeShellScript "openrgb-socket" ''
    set -uo pipefail
    sock="$1"

    ${lib.getExe pkgs.openrgb} --server --server-host 127.0.0.1 --server-port ${toString port} &
    server=$!

    # socat only dials the backend once a client shows up, but waiting for the
    # bind keeps the very first caller from racing startup.
    for ((i = 0; i < 100; i++)); do
      (exec 3<>/dev/tcp/127.0.0.1/${toString port}) 2>/dev/null && break
      ${pkgs.coreutils}/bin/sleep 0.1
    done

    ${pkgs.socat}/bin/socat \
      UNIX-LISTEN:"$sock",fork,mode=0600,unlink-early \
      TCP:127.0.0.1:${toString port} &
    bridge=$!

    # Either half on its own is useless, so exit and let systemd restart the pair.
    wait -n "$server" "$bridge"
    kill "$server" "$bridge" 2>/dev/null || true
  '';

  # openrgb.service is Type=simple, so systemd considers it started the moment
  # the script execs -- before the socket exists. Waiting here is quieter than
  # letting the effect fail and restart-loop through boot.
  waitForSocket = pkgs.writeShellScript "wait-for-openrgb-socket" ''
    for ((i = 0; i < 150; i++)); do
      [ -S "$1" ] && exit 0
      ${pkgs.coreutils}/bin/sleep 0.1
    done
    echo "openrgb socket $1 never appeared" >&2
    exit 1
  '';
in
{
  # The useful halves of services.hardware.openrgb, without its root unit.
  environment.systemPackages = [
    pkgs.openrgb
    rgb
  ];
  services.udev.packages = [ pkgs.openrgb ];

  # DRAM lighting sits on the SMBus. hardware.i2c.enable loads i2c-dev and
  # installs the rule giving /dev/i2c-* a uaccess ACL and group i2c; i2c-piix4
  # is the SMBus controller on AM5.
  hardware.i2c.enable = true;
  boot.kernelModules = [ "i2c-piix4" ];

  # uaccess already covers a seated user, but real group membership keeps
  # /dev/i2c-* reachable regardless of session state, so the DRAM modules
  # cannot be missed if the server starts before the seat ACLs land.
  users.users.${user}.extraGroups = [ "i2c" ];

  systemd.user.services.openrgb = {
    description = "OpenRGB SDK server (Unix socket only)";
    wantedBy = [ "default.target" ];

    # systemd.user units are generated for every user; this keeps it to one.
    unitConfig.ConditionUser = user;

    serviceConfig = {
      # Network isolation only: USB, hidraw and i2c are unaffected, so device
      # detection still finds everything.
      PrivateNetwork = true;
      ExecStart = "${runner} %t/openrgb.sock";
      Restart = "on-failure";
      RestartSec = 2;
    };
  };

  systemd.user.services.rgb-comet = {
    description = "Comet lighting effect in the shell palette";
    wantedBy = [ "default.target" ];
    after = [ "openrgb.service" ];

    # Without a server there is nothing to drive, and restarting the server
    # invalidates the connection, so tie the effect's life to it.
    bindsTo = [ "openrgb.service" ];

    unitConfig.ConditionUser = user;

    serviceConfig = {
      ExecStartPre = "${waitForSocket} %t/openrgb.sock";
      # Reads the palette from ~/.config/tabularium-imperium/colors.json and
      # re-themes itself whenever the shell rewrites it.
      ExecStart = "${lib.getExe rgb} effect comet";
      Restart = "always";
      RestartSec = 3;
    };
  };
}
