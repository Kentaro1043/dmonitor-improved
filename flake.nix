{
  description = "dmonitor-improved development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    { nixpkgs, ... }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      devShells = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          armhfCC = pkgs.pkgsCross.armv7l-hf-multiplatform.stdenv.cc;
          armhfGcc = pkgs.writeShellScriptBin "arm-linux-gnueabihf-gcc" ''
            exec ${armhfCC}/bin/${armhfCC.targetPrefix}gcc "$@"
          '';
        in
        {
          default = pkgs.mkShell {
            packages = [
              pkgs.cacert
              pkgs.coreutils
              pkgs.curl
              pkgs.file
              pkgs.findutils
              pkgs.gcc
              pkgs.git
              pkgs.gnumake
              pkgs.gnugrep
              pkgs.gnupg
              pkgs.go
              pkgs.nodejs_22
              pkgs.qemu
              pkgs.ripgrep
              armhfGcc
            ];

            shellHook = ''
              export CC_ARMHF=arm-linux-gnueabihf-gcc
              export GOTOOLCHAIN=local
              echo "dmonitor-improved devShell: go, npm, qemu-arm, gpg, file, and armhf gcc are available."
            '';
          };
        }
      );
    };
}
