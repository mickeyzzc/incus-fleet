#!/bin/sh
# Runs inside the new instance as root. Uses only documentation values.
set -eu

mkdir -p /srv/mibee-nvr/recordings
cat >/srv/mibee-nvr/config.env <<'EOF'
# Install values are injected by fleet into /etc/fleet-plugin/config.env.
EOF
cat >/srv/mibee-nvr/entry.sh <<'EOF'
#!/bin/sh
set -eu
cd /srv/mibee-nvr
exec python3 -m http.server 8080 --bind 0.0.0.0
EOF
chmod +x /srv/mibee-nvr/entry.sh
nohup /srv/mibee-nvr/entry.sh >/var/log/mibee-nvr.log 2>&1 &
