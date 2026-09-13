{
  outputs =
    { self }:
    {
      # devenv CLI consumers:
      #   inputs.obsidian = { url = "github:you/overlays"; flake = false; };
      #   imports = [ obsidian ];
      devenvModules.default = ./devenv.nix;

      # Nix flake consumers can build a single plugin derivation with:
      #   overlays.lib.mkObsidianPlugin pkgs { id = "symbat"; ... }
      lib.mkObsidianPlugin = pkgs: pkgs.callPackage ./lib/mk-obsidian-plugin.nix { };
    };
}
