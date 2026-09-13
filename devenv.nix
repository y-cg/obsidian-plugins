{ pkgs, ... }:

# Development environment for this repository itself.
#
# This is intentionally separate from the module consumers import. That module
# lives in nix/modules/obsidian.nix and is exposed as the `devenvModules.default`
# flake output; nothing here is part of its public interface.
{
  # Importing the module keeps it evaluated on every shell entry (it defines
  # options only, and `obsidian.enable` defaults to false, so no tasks run).
  imports = [ ./nix/modules/obsidian.nix ];

  # The CLI in cli/ is written in Go.
  languages.go.enable = true;

  packages = with pkgs; [
    git
    nixfmt
  ];

  enterShell = ''
    echo "obsidian.nix — module: nix/modules/obsidian.nix, CLI: cli/"
  '';

  enterTest = ''
    ( cd cli && go test ./... )
  '';
}
