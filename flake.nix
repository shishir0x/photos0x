{
  description = "photos0x: unofficial Google Photos client (GUI and CLI)";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      inherit (nixpkgs) lib;

      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      # vendorHash / npmDepsHash live in nix/hashes.json and are refreshed with
      # nix/update-hashes.sh (see nix/README.md).
      hashes = builtins.fromJSON (builtins.readFile ./nix/hashes.json);

      # Same file the binary embeds (core.GetVersion), so the package version
      # can never drift from what `photos0x` reports.
      version = (builtins.fromJSON (builtins.readFile ./build/windows/info.json)).fixed.file_version;

      src = lib.fileset.toSource {
        root = ./.;
        fileset = lib.fileset.unions [
          ./go.mod
          ./go.sum
          ./main.go
          ./main_cli.go
          ./version.go
          ./core
          ./generated
          ./internal
          ./build/windows/info.json
          ./build/appicon.png
        ];
      };

      mkPackages =
        pkgs:
        let
          # go.mod asks for Go 1.27; fall back to the default Go if nixpkgs has no go_1_27 yet.
          buildGoModule = pkgs.buildGoModule.override { go = pkgs.go_1_27 or pkgs.go; };

          common = {
            inherit version src;
            vendorHash = hashes.vendorHash;
            ldflags = [
              "-s"
              "-w"
            ];
            # Tests touch the network and the user's config dir; run them in CI instead.
            doCheck = false;
          };

          meta = {
            description = "Unofficial Google Photos client";
            homepage = "https://github.com/shishir0x/photos0x";
            license = lib.licenses.mit;
            platforms = lib.platforms.linux;
          };
        in
        rec {
          # Vue frontend, embedded into the GUI binary (main.go: //go:embed all:frontend/dist).
          # Wails bindings are committed in frontend/bindings, so wails3 is not needed here.
          frontend = pkgs.buildNpmPackage {
            pname = "photos0x-frontend";
            inherit version;
            src = ./frontend;
            nodejs = pkgs.nodejs_24; # frontend/.node-version
            npmDepsHash = hashes.npmDepsHash;
            installPhase = ''
              runHook preInstall
              cp -r dist $out
              runHook postInstall
            '';
          };

          # Wails v3 GUI (GTK4 + WebKitGTK 6.0). Also accepts the CLI subcommands.
          photos0x = buildGoModule (
            common
            // {
              pname = "photos0x";
              tags = [ "production" ];

              nativeBuildInputs = [
                pkgs.pkg-config
                pkgs.wrapGAppsHook4
                pkgs.copyDesktopItems
              ];
              buildInputs = [
                pkgs.gtk4
                pkgs.webkitgtk_6_0
                pkgs.libsoup_3
                pkgs.glib-networking # TLS for WebKit
              ];

              preBuild = ''
                mkdir -p frontend
                cp -r ${frontend} frontend/dist
                chmod -R u+w frontend/dist
              '';

              postInstall = ''
                mv $out/bin/app $out/bin/photos0x
                ln -s $out/bin/photos0x $out/bin/gotohp
                install -Dm444 build/appicon.png $out/share/icons/hicolor/128x128/apps/photos0x.png
              '';

              # Wails registers the GTK app id as org.wails.<sanitised app name>
              # ("com_shishir0x_photos0x"); matching it lets Wayland compositors find the icon.
              desktopItems = [
                (pkgs.makeDesktopItem {
                  name = "org.wails.com_shishir0x_photos0x";
                  desktopName = "photos0x";
                  comment = "Google Photos Unofficial Client";
                  exec = "photos0x";
                  icon = "photos0x";
                  categories = [ "Utility" ];
                  startupWMClass = "org.wails.com_shishir0x_photos0x";
                })
              ];

              meta = meta // {
                mainProgram = "photos0x";
              };
            }
          );

          # CLI only: built with -tags cli, no Wails / WebKit / cgo.
          photos0x-cli = buildGoModule (
            common
            // {
              pname = "photos0x-cli";
              tags = [ "cli" ];
              env.CGO_ENABLED = "0";

              postInstall = ''
                mv $out/bin/app $out/bin/photos0x-cli
                ln -s $out/bin/photos0x-cli $out/bin/gotohp-cli
              '';

              meta = meta // {
                mainProgram = "photos0x-cli";
              };
            }
          );

          gotohp = photos0x;
          gotohp-cli = photos0x-cli;

          default = photos0x;
        };
    in
    {
      packages = forAllSystems mkPackages;

      apps = forAllSystems (pkgs: {
        default = {
          type = "app";
          program = lib.getExe self.packages.${pkgs.stdenv.hostPlatform.system}.photos0x;
        };
        cli = {
          type = "app";
          program = lib.getExe self.packages.${pkgs.stdenv.hostPlatform.system}.photos0x-cli;
        };
      });

      overlays.default = final: _prev: {
        inherit (mkPackages final) photos0x photos0x-cli gotohp gotohp-cli;
      };

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          inputsFrom = [ self.packages.${pkgs.stdenv.hostPlatform.system}.photos0x ];
          packages = [
            (pkgs.go_1_27 or pkgs.go)
            pkgs.nodejs_24
            pkgs.go-task
            pkgs.gofumpt
            pkgs.golangci-lint
          ];
        };
      });

      checks = forAllSystems (pkgs: {
        inherit (self.packages.${pkgs.stdenv.hostPlatform.system}) photos0x photos0x-cli frontend;
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-rfc-style);
    };
}
