package migrations

import (
	"context"
	"fmt"
	"strings"
)

// GuestOps runs a maintenance action inside a guest and returns its output.
// It mirrors the shape of the instance ops service without importing it, so
// this package stays free of a cycle.
type GuestOps interface {
	RunScript(ctx context.Context, cluster, instance, script string) (stdout string, stderr string, exitCode int, err error)
}

// NetworkFixer restores a guest's network after a cross-cluster move.
//
// Incus bridge networks are per-member, so a moved guest keeps the DHCP lease
// it obtained on the source cluster. RFC 2131 lets a client keep using that
// address until the lease expires (Incus defaults to one hour), and the target
// cluster's DHCP server cannot see the old lease, so the guest stays unreachable
// for up to an hour. Forcing a release/renew inside the guest makes the target
// DHCP server issue an address from its own subnet immediately.
type NetworkFixer struct {
	ops GuestOps
	log logger
}

// logger is the subset of slog.Logger this file needs.
type logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
}

// NewNetworkFixer builds a fixer around the guest ops service.
func NewNetworkFixer(ops GuestOps, log logger) *NetworkFixer {
	return &NetworkFixer{ops: ops, log: log}
}

// renewScript releases the current lease and requests a new one. It covers the
// common guest tooling (dhclient, udhcpc, systemd-networkd, NetworkManager) and
// exits non-zero only when none of them are present.
const renewScript = `set -u
renewed=0

if command -v dhclient >/dev/null 2>&1; then
  dhclient -r >/dev/null 2>&1 || true
  dhclient >/dev/null 2>&1 && renewed=1
fi

if [ "$renewed" -eq 0 ] && command -v udhcpc >/dev/null 2>&1; then
  udhcpc -i "$(ip -o -4 route show default | awk '{print $5; exit}')" -n -q >/dev/null 2>&1 && renewed=1
fi

if [ "$renewed" -eq 0 ] && command -v systemctl >/dev/null 2>&1; then
  systemctl restart systemd-networkd >/dev/null 2>&1 && renewed=1
  if [ "$renewed" -eq 0 ]; then
    systemctl restart NetworkManager >/dev/null 2>&1 && renewed=1
  fi
fi

if [ "$renewed" -eq 0 ]; then
  echo "no supported DHCP client found" >&2
  exit 1
fi

# Report the address so the caller can confirm the guest landed on the target
# subnet rather than reusing the old one.
ip -o -4 addr show scope global | awk '{print $2, $4}'
`

// RestoreLease asks the guest to drop its old DHCP lease and take a new one from
// the cluster it now lives on. It is best-effort: a guest without any DHCP
// client, or an instance that is not running, must not fail the migration.
func (f *NetworkFixer) RestoreLease(ctx context.Context, cluster, instance string) error {
	if f == nil || f.ops == nil {
		return nil
	}

	stdout, stderr, code, err := f.ops.RunScript(ctx, cluster, instance, renewScript)
	if err != nil {
		return fmt.Errorf("migrations: renew lease for %s:%s: %w", cluster, instance, err)
	}
	if code != 0 {
		// A guest using a static address, or one without a DHCP client, lands
		// here. That is expected, so log it and let the migration succeed.
		f.log.Warn("guest did not renew its DHCP lease",
			"cluster", cluster, "instance", instance, "exit", code, "stderr", strings.TrimSpace(stderr))
		return nil
	}

	f.log.Info("guest renewed its DHCP lease",
		"cluster", cluster, "instance", instance, "addresses", strings.TrimSpace(stdout))

	return nil
}

// GuestOpsFunc adapts a function to the GuestOps interface.
type GuestOpsFunc func(ctx context.Context, cluster, instance, script string) (stdout string, stderr string, exitCode int, err error)

// RunScript implements GuestOps.
func (f GuestOpsFunc) RunScript(ctx context.Context, cluster, instance, script string) (string, string, int, error) {
	return f(ctx, cluster, instance, script)
}
