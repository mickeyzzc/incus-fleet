#!/bin/sh
# Runs inside the new instance as root. Exit non-zero to fail the install and
# make fleet delete the instance again.
set -eu

apt-get update -qq
apt-get install -y -qq python3 >/dev/null
mkdir -p /srv/hello
cat >/srv/hello/index.html <<'EOF'
hello from a fleet plugin
EOF
cd /srv/hello
nohup python3 -m http.server 8080 --bind 0.0.0.0 >/var/log/hello.log 2>&1 &
