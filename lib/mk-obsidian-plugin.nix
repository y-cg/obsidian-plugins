{
  lib,
  fetchurl,
  runCommandLocal,
  jq,
}:
{
  id,
  repo,
  version,
  assets,
  description ? "Obsidian community plugin: ${id}",
}:
let
  assetDrvs = lib.mapAttrs
    (fname: hash:
      fetchurl {
        url = "https://github.com/${repo}/releases/download/${version}/${fname}";
        inherit hash;
        name = "obsidian-${id}-${version}-${fname}";
      })
    assets;
in
runCommandLocal "obsidian-plugin-${id}-${version}" {
  passthru = {
    inherit
      id
      repo
      version
      assets
      ;
  };
  meta = {
    inherit description;
    homepage = "https://github.com/${repo}";
  };
} ''
  mkdir -p $out

  ${lib.concatMapStringsSep "\n"
    (fname: ''cp -L ${assetDrvs.${fname}} "$out/${fname}"'')
    (builtins.attrNames assets)}

  if [ ! -f "$out/main.js" ]; then
    echo "obsidian plugin '${id}': assets must include main.js" >&2
    exit 1
  fi
  if [ ! -f "$out/manifest.json" ]; then
    echo "obsidian plugin '${id}': assets must include manifest.json" >&2
    exit 1
  fi

  manifest_id=$(${jq}/bin/jq -r '.id' "$out/manifest.json")
  if [ "$manifest_id" != "${id}" ]; then
    echo "obsidian plugin '${id}': manifest.json declares id '$manifest_id'" >&2
    exit 1
  fi
''
