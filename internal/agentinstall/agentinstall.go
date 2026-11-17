// Package agentinstall serves the self-contained Linux installer used by the
// agent-first Add node flow. The center can host the agent/helper binaries,
// CA certificate, sudoers file, and systemd unit, so a fresh host only needs
// curl and one command.
package agentinstall

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"
)

//go:embed assets/agent-sudoers
var sudoersAsset string

//go:embed assets/incus-fleet-agent.service
var unitAsset string

// Config controls the generated installer and the binary endpoints.
type Config struct {
	// GatewayURL is the public mTLS agent gateway, for example
	// https://fleet.example:8444. It is embedded in the install command.
	GatewayURL string
	// AssetURL is the public base URL of the main HTTP API. When empty the
	// installer derives it from the request (honoring X-Forwarded-* headers).
	AssetURL string
	// AgentBinary and HelperBinary are Linux binaries matching the target
	// architecture. They are checksummed at startup.
	AgentBinary  string
	HelperBinary string
}

// Handler serves the installer and its assets.
type Handler struct {
	cfg    Config
	caPEM  string
	agent  asset
	helper asset
	logger *slog.Logger
}

type asset struct {
	path   string
	size   int64
	sha256 string
}

// New validates the configuration, hashes the binaries, and prepares the
// installer. It is called only when agent.install.enabled is true.
func New(cfg Config, caPEM []byte, logger *slog.Logger) (*Handler, error) {
	cfg.GatewayURL = strings.TrimRight(strings.TrimSpace(cfg.GatewayURL), "/")
	cfg.AssetURL = strings.TrimRight(strings.TrimSpace(cfg.AssetURL), "/")
	if cfg.GatewayURL == "" {
		return nil, fmt.Errorf("agentinstall: gateway_url is required")
	}
	gateway, err := url.Parse(cfg.GatewayURL)
	if err != nil || gateway.Scheme != "https" || gateway.Host == "" {
		return nil, fmt.Errorf("agentinstall: gateway_url must be an https URL")
	}
	if cfg.AssetURL != "" {
		assetURL, err := url.Parse(cfg.AssetURL)
		if err != nil || (assetURL.Scheme != "https" && assetURL.Scheme != "http") || assetURL.Host == "" {
			return nil, fmt.Errorf("agentinstall: asset_url must be an http(s) URL")
		}
	}
	if len(bytes.TrimSpace(caPEM)) == 0 {
		return nil, fmt.Errorf("agentinstall: CA certificate is required")
	}
	agent, err := loadAsset(cfg.AgentBinary, "agent_binary")
	if err != nil {
		return nil, err
	}
	helper, err := loadAsset(cfg.HelperBinary, "helper_binary")
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Handler{
		cfg: cfg, caPEM: string(caPEM), agent: agent, helper: helper, logger: logger,
	}, nil
}

// Register mounts the public installer routes. They intentionally do not
// require a session: the one-time enrollment token is the credential.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/agent/install.sh", h.installScript)
	mux.HandleFunc("GET /api/v1/agent/install/agent", h.serveAgent)
	mux.HandleFunc("GET /api/v1/agent/install/helper", h.serveHelper)
	mux.HandleFunc("GET /api/v1/agent/install/ca.crt", h.serveCA)
}

func (h *Handler) installScript(w http.ResponseWriter, r *http.Request) {
	base := h.cfg.AssetURL
	if base == "" {
		base = requestBaseURL(r)
	}
	if base == "" {
		http.Error(w, "installer is not configured with a public URL", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := installTemplate.Execute(w, map[string]string{
		"AssetURL":    shQuote(base),
		"AssetURLRaw": base,
		"GatewayURL":  shQuote(h.cfg.GatewayURL),
		"AgentSHA":    h.agent.sha256,
		"HelperSHA":   h.helper.sha256,
		"CAPEM":       h.caPEM,
		"Sudoers":     sudoersAsset,
		"Unit":        unitAsset,
	}); err != nil {
		h.logger.Error("render agent installer", "error", err)
	}
}

func (h *Handler) serveAgent(w http.ResponseWriter, _ *http.Request) {
	h.serveAsset(w, h.agent, "incus-fleet-agent")
}

func (h *Handler) serveHelper(w http.ResponseWriter, _ *http.Request) {
	h.serveAsset(w, h.helper, "incus-fleet-helper")
}

func (h *Handler) serveCA(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(h.caPEM)))
	_, _ = io.WriteString(w, h.caPEM)
}

func (h *Handler) serveAsset(w http.ResponseWriter, a asset, name string) {
	file, err := os.Open(a.path) //nolint:gosec // path is operator-configured
	if err != nil {
		http.Error(w, "installer asset is unavailable", http.StatusServiceUnavailable)
		return
	}
	defer func() { _ = file.Close() }()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+name)
	w.Header().Set("Content-Length", strconv.FormatInt(a.size, 10))
	w.Header().Set("X-Checksum-Sha256", a.sha256)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, &http.Request{Method: http.MethodGet}, name, modTime(a.path), file)
}

func loadAsset(path, field string) (asset, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return asset{}, fmt.Errorf("agentinstall: %s is required", field)
	}
	file, err := os.Open(path) //nolint:gosec // operator-configured path
	if err != nil {
		return asset{}, fmt.Errorf("agentinstall: open %s: %w", field, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return asset{}, fmt.Errorf("agentinstall: stat %s: %w", field, err)
	}
	if !info.Mode().IsRegular() {
		return asset{}, fmt.Errorf("agentinstall: %s is not a regular file", field)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return asset{}, fmt.Errorf("agentinstall: hash %s: %w", field, err)
	}

	return asset{path: path, size: info.Size(), sha256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func modTime(path string) (zeroTime time.Time) {
	info, err := os.Stat(path)
	if err != nil {
		return zeroTime
	}

	return info.ModTime()
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		if first, _, found := strings.Cut(forwarded, ","); found {
			forwarded = first
		}
		if forwarded = strings.TrimSpace(forwarded); forwarded == "http" || forwarded == "https" {
			scheme = forwarded
		}
	}
	host := strings.TrimSpace(r.Host)
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		if first, _, found := strings.Cut(forwarded, ","); found {
			forwarded = first
		}
		if forwarded = strings.TrimSpace(forwarded); forwarded != "" {
			host = forwarded
		}
	}
	if host == "" {
		return ""
	}

	return scheme + "://" + host
}

func shQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

var installTemplate = template.Must(template.New("install").Parse(`#!/bin/sh
# Incus Fleet agent installer. Run as root on a Linux host with systemd:
#   curl -fsSL {{.AssetURLRaw}}/api/v1/agent/install.sh | sudo sh -s -- \
#     --token <enrollment-token> --cluster <cluster> --member <member>
set -eu

ASSET_URL={{.AssetURL}}
CENTER_URL={{.GatewayURL}}

TOKEN=''
CLUSTER=''
MEMBER=''

usage() {
	cat <<'USAGE'
Usage: install.sh --token TOKEN --cluster NAME --member NAME
  --token    one-time enrollment token from the center
  --cluster  cluster name bound to the token
  --member   member name bound to the token
USAGE
}

while [ $# -gt 0 ]; do
	case "$1" in
		--token) TOKEN="${2:-}"; shift 2 ;;
		--cluster) CLUSTER="${2:-}"; shift 2 ;;
		--member) MEMBER="${2:-}"; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
	esac
done

if [ "$(id -u)" -ne 0 ]; then
	echo "run this installer as root (sudo sh -s -- ...)" >&2
	exit 1
fi
if [ -z "$TOKEN" ] || [ -z "$CLUSTER" ] || [ -z "$MEMBER" ]; then
	usage >&2
	exit 2
fi
for command in curl sha256sum systemctl runuser useradd groupadd visudo; do
	if ! command -v "$command" >/dev/null 2>&1; then
		echo "required command is missing: $command" >&2
		exit 1
	fi
done

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

echo "downloading incus-fleet-agent"
curl -fsSL "$ASSET_URL/api/v1/agent/install/agent" -o "$TMP/incus-fleet-agent"
printf '%s  %s\n' '{{.AgentSHA}}' "$TMP/incus-fleet-agent" | sha256sum -c - >/dev/null
echo "downloading incus-fleet-helper"
curl -fsSL "$ASSET_URL/api/v1/agent/install/helper" -o "$TMP/incus-fleet-helper"
printf '%s  %s\n' '{{.HelperSHA}}' "$TMP/incus-fleet-helper" | sha256sum -c - >/dev/null

install -m 0755 "$TMP/incus-fleet-agent" /usr/local/bin/incus-fleet-agent
install -m 0755 "$TMP/incus-fleet-helper" /usr/libexec/incus-fleet-helper

install -d -m 0755 /etc/incus-fleet
cat > /etc/incus-fleet/agent-ca.crt <<'FLEET_CA'
{{.CAPEM}}
FLEET_CA
chmod 0644 /etc/incus-fleet/agent-ca.crt

cat > /etc/sudoers.d/incus-fleet-agent <<'FLEET_SUDOERS'
{{.Sudoers}}
FLEET_SUDOERS
chmod 0440 /etc/sudoers.d/incus-fleet-agent
chown root:root /etc/sudoers.d/incus-fleet-agent
visudo -cf /etc/sudoers.d/incus-fleet-agent >/dev/null

if ! getent group incus-fleet-agent >/dev/null 2>&1; then
	groupadd --system incus-fleet-agent
fi
if ! id -u incus-fleet-agent >/dev/null 2>&1; then
	useradd --system --gid incus-fleet-agent --home-dir /var/lib/incus-fleet-agent \
		--shell /usr/sbin/nologin incus-fleet-agent
fi
install -d -m 0700 -o incus-fleet-agent -g incus-fleet-agent /var/lib/incus-fleet-agent
install -d -m 0700 -o incus-fleet-agent -g incus-fleet-agent /var/lib/incus-fleet-helper

cat > /etc/systemd/system/incus-fleet-agent.service <<'FLEET_UNIT'
{{.Unit}}
FLEET_UNIT
systemctl daemon-reload

if runuser -u incus-fleet-agent -- /usr/local/bin/incus-fleet-agent status \
	--state-dir /var/lib/incus-fleet-agent >/dev/null 2>&1; then
	echo "existing agent identity found; skipping enrollment"
else
	printf '%s' "$TOKEN" | runuser -u incus-fleet-agent -- \
		/usr/local/bin/incus-fleet-agent enroll \
		--center "$CENTER_URL" \
		--ca /etc/incus-fleet/agent-ca.crt \
		--cluster "$CLUSTER" \
		--member "$MEMBER" \
		--state-dir /var/lib/incus-fleet-agent
fi

systemctl enable --now incus-fleet-agent
echo "incus-fleet-agent installed and enrolled"
systemctl --no-pager --lines=0 status incus-fleet-agent || true
`))
