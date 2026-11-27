#!/usr/bin/env bash
# Package a fleet service plugin for the plugin store.
#
# Usage:
#   scripts/pack-plugin.sh <plugin-dir> [output.fleetplugin]
#
# <plugin-dir> contains manifest.yaml (preferred) or plugin.json (legacy) plus
# the install script it references. The result is a tar.gz whose root is the
# plugin directory; extract it into fleet's plugins.dir/<id>/.
#
# Set FLEET_PLUGIN_SIGNING_KEY (or INCUS_FLEET_PLUGINS_SIGNING_KEY) to write a
# manifest.yaml.sidecar signature. Community catalog entries are rejected by
# fleet when no valid signature is present and a signing key is configured.
set -euo pipefail

src="${1:-}"
if [ -z "$src" ] || [ ! -d "$src" ]; then
  echo "usage: $0 <plugin-dir> [output.fleetplugin]" >&2
  exit 2
fi

manifest="$src/manifest.yaml"
legacy="$src/plugin.json"
if [ ! -f "$manifest" ]; then
  if [ ! -f "$legacy" ]; then
    echo "error: neither $manifest nor $legacy was found" >&2
    exit 1
  fi
  manifest="$legacy"
fi

python3 - "$manifest" "$src" <<'PY'
import os
import re
import sys

path, root = sys.argv[1], sys.argv[2]
with open(path, encoding="utf-8") as fh:
    raw = fh.read()

errors = []
if path.endswith(".yaml"):
    def field(name):
        match = re.search(r"^" + re.escape(name) + r":\s*(.+?)\s*$", raw, re.M)
        return match.group(1).strip() if match else ""
    plugin_id = field("id")
    if not re.fullmatch(r"[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?", plugin_id):
        errors.append("id must be a lowercase DNS label")
    if field("schema") != "fleet-plugin/v1":
        errors.append("schema must be fleet-plugin/v1")
    if not field("name"):
        errors.append("name is required")
    if field("tier") not in ("official", "community"):
        errors.append("tier must be official or community")
    if "image:" not in raw or "reference:" not in raw:
        errors.append("artifact.image.reference is required")
    script = "install.sh"
else:
    import json
    doc = json.loads(raw)
    plugin_id = str(doc.get("id", ""))
    if not re.fullmatch(r"[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?", plugin_id):
        errors.append("id must be a lowercase DNS label")
    if not str(doc.get("name", "")).strip():
        errors.append("name is required")
    if not str(doc.get("ui_path", "")).startswith("/plugin/"):
        errors.append("ui_path must start with /plugin/")
    image = str(doc.get("image", "")).strip()
    if image:
        for key in ("default_cpu", "default_memory", "default_disk", "default_network"):
            if not str(doc.get(key, "")).strip():
                errors.append(f"{key} is required when image is set")
        if not isinstance(doc.get("port"), int) or not (1 <= doc["port"] <= 65535):
            errors.append("port must be an integer between 1 and 65535")
        script = str(doc.get("install_script_file", "")).strip()
        if not script:
            errors.append("install_script_file is required when image is set")
    elif not str(doc.get("upstream_hint", "")).startswith(("http://", "https://")):
        errors.append("upstream_hint must be an http(s) URL for connect-only plugins")

if errors:
    for err in errors:
        print(f"error: {err}", file=sys.stderr)
    sys.exit(1)
if "install.sh" in locals() and not os.path.isfile(os.path.join(root, "install.sh")):
    print("error: install.sh not found", file=sys.stderr)
    sys.exit(1)
print(f"validated {plugin_id}")
PY

# Sign the exact bytes that fleet verifies. Documentation key values are never
# embedded in the repository; the operator provides them at pack time.
signing_key="${FLEET_PLUGIN_SIGNING_KEY:-${INCUS_FLEET_PLUGINS_SIGNING_KEY:-}}"
if [ -n "$signing_key" ] && [ -f "$manifest" ] && [[ "$manifest" == *.yaml ]]; then
  if ! command -v openssl >/dev/null 2>&1; then
    echo "error: openssl is required to sign a plugin package" >&2
    exit 1
  fi
  openssl dgst -sha256 -hmac "$signing_key" -binary "$manifest" | base64 >"$manifest.sig"
  echo "signed $(basename "$manifest")"
fi

name="$(basename "$src")"
out="${2:-dist/${name}.fleetplugin}"
mkdir -p "$(dirname "$out")"
tar -czf "$out" -C "$src" .
echo "packed $out"
