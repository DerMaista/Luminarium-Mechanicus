{ lib, buildGoModule }:

buildGoModule {
  pname = "rgb";
  version = "0.1.0";

  # Only the files the compiler actually reads, so editing the README or the
  # nix expressions does not invalidate the build.
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      (lib.fileset.fileFilter (f: f.hasExt "go") ../.)
    ];
  };

  # Nothing third-party is imported, so there is no go.sum and nothing to
  # vendor. null tells buildGoModule to skip the vendor step entirely.
  vendorHash = null;

  # The Go module is named after the repo, but the command is called rgb.
  postInstall = ''
    mv "$out/bin/luminarium-mechanicus" "$out/bin/rgb"
  '';

  meta = {
    description = "Drive PC RGB lighting through an OpenRGB SDK server";
    mainProgram = "rgb";
    platforms = lib.platforms.linux;
  };
}
