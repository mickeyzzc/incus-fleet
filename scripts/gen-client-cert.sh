#!/usr/bin/env bash
# Generate a self-signed client certificate for incus-fleet.
# The certificate is safe to trust on every Incus cluster the app connects to.
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <common-name> [output-dir]" >&2
  exit 2
fi

name="$1"
outdir="${2:-.}"

if [[ ! "$name" =~ ^[A-Za-z0-9._-]+$ ]]; then
  echo "common-name may only contain letters, digits, dot, underscore, and dash" >&2
  exit 2
fi

mkdir -p "$outdir"
umask 077

openssl req \
  -x509 \
  -newkey rsa:4096 \
  -sha256 \
  -nodes \
  -days 3650 \
  -subj "/CN=${name}" \
  -addext "keyUsage=critical,digitalSignature,keyEncipherment" \
  -addext "extendedKeyUsage=clientAuth" \
  -keyout "${outdir}/${name}.key" \
  -out "${outdir}/${name}.crt"

chmod 600 "${outdir}/${name}.key"
chmod 644 "${outdir}/${name}.crt"

echo "wrote ${outdir}/${name}.crt"
echo "wrote ${outdir}/${name}.key"
