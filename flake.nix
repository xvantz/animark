{
  description = "animark — self-hosted, git-synced anime tracker";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        goVersion = "1.25";
        go = pkgs."go_${lib.replaceStrings ["."] ["_"] goVersion}";
        lib = pkgs.lib;
      in
      {
        packages.default = pkgs.buildGoModule rec {
          pname = "animark";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="; # will be set after first build
          subPackages = [ "cmd/animark" ];
          ldflags = [ "-s" "-w" "-X main.version=${version}" ];
          meta = {
            description = "Self-hosted, git-synced anime tracker";
            homepage = "https://github.com/xvantz/animark";
            license = lib.licenses.mit;
            maintainers = with lib.maintainers; [ xvantz ];
          };
        };

        nixosModules.default = { config, lib, pkgs, ... }:
          with lib;
          let
            cfg = config.services.animark;
            pkg = self.packages.${system}.default;
          in
          {
            options.services.animark = {
              enable = mkEnableOption "animark anime tracker";
              package = mkOption {
                type = types.package;
                default = pkg;
                description = "animark package to use";
              };
              address = mkOption {
                type = types.str;
                default = "127.0.0.1";
                description = "Listen address";
              };
              port = mkOption {
                type = types.port;
                default = 8080;
                description = "Listen port";
              };
              dataDir = mkOption {
                type = types.path;
                default = "/var/lib/animark";
                description = "Data directory for anime.json and .git";
              };
              gitRemote = mkOption {
                type = types.str;
                default = "";
                description = "Remote git URL for sync";
              };
              gitBranch = mkOption {
                type = types.str;
                default = "main";
                description = "Git branch";
              };
              gitPush = mkOption {
                type = types.bool;
                default = false;
                description = "Enable auto-push to remote";
              };
              user = mkOption {
                type = types.str;
                default = "animark";
                description = "System user to run the service";
              };
              group = mkOption {
                type = types.str;
                default = "animark";
                description = "System group";
              };
            };

            config = mkIf cfg.enable {
              users.users.${cfg.user} = {
                isSystemUser = true;
                group = cfg.group;
                home = cfg.dataDir;
                createHome = true;
              };
              users.groups.${cfg.group} = {};

              systemd.services.animark = {
                description = "animark — anime tracker";
                after = [ "network.target" ];
                wantedBy = [ "multi-user.target" ];
                serviceConfig = {
                  User = cfg.user;
                  Group = cfg.group;
                  WorkingDirectory = cfg.dataDir;
                  ExecStart = "${cfg.package}/bin/animark" +
                    " -addr ${cfg.address}:${toString cfg.port}" +
                    " -data ${cfg.dataDir}" +
                    optionalString (cfg.gitRemote != "") " -git-remote ${cfg.gitRemote}" +
                    " -git-branch ${cfg.gitBranch}" +
                    optionalString cfg.gitPush " -git-push";
                  Restart = "always";
                  RestartSec = "5";
                  StateDirectory = "animark";
                  StateDirectoryMode = "0755";
                  NoNewPrivileges = true;
                };
              };
            };
          };

        checks = {
          build = self.packages.${system}.default;
          test = pkgs.runCommand "animark-tests" {
            buildInputs = [ go ];
            src = self;
          } ''
            export HOME=$TMPDIR
            export GOMODCACHE=$TMPDIR/go-mod-cache
            cp -r $src src
            cd src
            go test ./... -count=1 -timeout 60s
            touch $out
          '';
        };

        devShells.default = pkgs.mkShell {
          packages = [ go gopls gotools ];
        };
      }
    );
}
