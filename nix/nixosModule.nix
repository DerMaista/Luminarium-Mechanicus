{ config, lib, pkgs, ... }:

let
  user = "christoph";

  port = 6742;

  rgb = pkgs.callPackage ./package.nix { };

  runner = pkgs.writeShellScriptBin "openrgb-socket" ''
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
  environment.systemPackages = [
    pkgs.openrgb
    rgb
    runner
  ];
  services.udev.packages = [ pkgs.openrgb ];

  hardware.i2c.enable = true;
  boot.kernelModules = [ "i2c-piix4" ];

  users.users.${user}.extraGroups = [ "i2c" ];

  systemd.user.services.openrgb = {
    description = "OpenRGB SDK server (Unix socket only)";
    wantedBy = [ "default.target" ];

    unitConfig.ConditionUser = user;

    serviceConfig = {
      PrivateNetwork = true;
      ExecStart = "${lib.getExe runner} %t/openrgb.sock";
      Restart = "on-failure";
      RestartSec = 2;
    };
  };

  systemd.user.services.rgb-comet = {
    description = "Comet lighting effect in the shell palette";
    wantedBy = [ "default.target" ];
    after = [ "openrgb.service" ];

    bindsTo = [ "openrgb.service" ];

    unitConfig.ConditionUser = user;

    serviceConfig = {
      ExecStartPre = "${waitForSocket} %t/openrgb.sock";
      ExecStart = "${lib.getExe rgb} effect comet";
      Restart = "always";
      RestartSec = 3;
    };
  };
}
