{ lib, buildGoModule }:

buildGoModule {
  pname = "obsidian-plugins";
  version = "0.1.0";

  src = ../cli;
  vendorHash = "sha256-RHUluEZkch+fsg7XB/WsuMsEXpdl3aqCfI4dMmbkCeg=";

  doCheck = true;

  meta = {
    description = "Declaratively install and synchronize Obsidian community plugins";
    mainProgram = "obsidian-plugins";
    platforms = lib.platforms.unix;
  };
}
