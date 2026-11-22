#!/usr/bin/env bash
# Install the repository's git hooks. Run once after cloning.
#
#   scripts/install-hooks.sh     (or: make hooks)

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$repo_root"

if [ ! -d .githooks ]; then
	echo "install-hooks: .githooks/ not found" >&2
	exit 1
fi

chmod +x .githooks/*
git config core.hooksPath .githooks

echo "install-hooks: core.hooksPath = .githooks"
echo "install-hooks: installed:"
ls -1 .githooks | sed 's/^/  /'
