{
  description = "dmonitor-improved development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = {nixpkgs, ...}: let
    systems = [
      "x86_64-linux"
      "aarch64-linux"
      "aarch64-darwin"
    ];
    forAllSystems = nixpkgs.lib.genAttrs systems;
  in {
    devShells = forAllSystems (
      system: let
        pkgs = import nixpkgs {inherit system;};
        armhfCC = pkgs.pkgsCross.armv7l-hf-multiplatform.stdenv.cc;
        armhfGcc = pkgs.writeShellScriptBin "arm-linux-gnueabihf-gcc" ''
          exec ${armhfCC}/bin/${armhfCC.targetPrefix}gcc "$@"
        '';
        linuxOnlyPackages = pkgs.lib.optionals pkgs.stdenv.isLinux [
          pkgs.qemu
        ];
        qemuNote =
          if pkgs.stdenv.isLinux
          then "qemu-arm"
          else "qemu-arm is unavailable on Darwin; run the official armhf binaries inside Linux";
      in {
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
            pkgs.ripgrep
            armhfGcc
          ] ++ linuxOnlyPackages;

          shellHook = ''
            export CC_ARMHF=arm-linux-gnueabihf-gcc
            export GOTOOLCHAIN=local
            echo "dmonitor-improved devShell: go, npm, gpg, file, armhf gcc, and ${qemuNote}."
          '';
        };
      }
    );
  };
}
