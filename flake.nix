{
  description = "Flake";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
  };

  outputs = inputs@{ self, flake-parts, ... }:
  flake-parts.lib.mkFlake { inherit inputs; } {

    flake = {
        nixosModules.default = import ./nix/nixosModule.nix;
      };

    systems = [
      "x86_64-linux"
      "aarch64-linux"
    ];

    perSystem = { config, pkgs, ... }: {
      packages.rgb = pkgs.callPackage ./nix/package.nix { };
      packages.default = config.packages.rgb;
    };
  };
}
