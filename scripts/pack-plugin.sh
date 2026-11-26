#!/usr/bin/env bash
# Package a fleet service plugin for the plugin store.
#
# Usage:
#   scripts/pack-plugin.sh <plugin-dir> [output.fleetplugin]
#
# <plugin-dir> must contain plugin.json and, when it deploys an instance, the
# install script it references. The resulting .fleetplugin file is a tar.gz
# whose root is the plugin directory; extract it into fleet's plugins.dir.
set -euo pipefail

src="${1:-}"
if [ -z "$src" ] || [ ! -d "$src" ]; then
  echo "usage: $0 <plugin-dir> [output.fleetplugin]" >&2
  exit 2
fi

manifest="$src/plugin.json"
if [ ! -f "$manifest" ]; then
  echo "error: $manifest not found" >&2
  exit 1
fi

python3 - "$manifest" "$src" <<'PY'
import json
import os
import re
import sys

manifest_path, root = sys.argv[1], sys.argv[2]
with open(manifest_path, encoding="utf-8") as fh:
    doc = json.load(fh)

errors = []
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
    elif not os.path.isfile(os.path.join(root, script)):
        errors.append(f"install script not found: {script}")
elif not str(doc.get("upstream_hint", "")).startswith(("http://", "https://")):
    errors.append("upstream_hint must be an http(s) URL for connect-only plugins")

if errors:
    for err in errors:
        print(f"error: {err}", file=sys.stderr)
    sys.exit(1)
print(f"validated {plugin_id}")
PY

name="$(basename "$src")"
out="${2:-dist/${name}.fleetplugin}"
mkdir -p "$(dirname "$out")"
tar -czf "$out" -C "$src" .
echo "packed $out"
