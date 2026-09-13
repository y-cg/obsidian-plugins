{
  outputs =
    { self }:
    {
      # devenv consumers:
      #   inputs.obsidian.url = "github:y-cg/obsidian.nix";
      #   imports = [ inputs.obsidian.devenvModules.default ];
      devenvModules.default = ./nix/modules/obsidian.nix;

      # Nix flake consumers can build a single plugin derivation with:
      #   overlays.lib.mkObsidianPlugin pkgs { id = "symbat"; ... }
      lib.mkObsidianPlugin = pkgs: pkgs.callPackage ./lib/mk-obsidian-plugin.nix { };
    };
}
