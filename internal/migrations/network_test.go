package migrations

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeGuestOps records the script it was asked to run.
type fakeGuestOps struct {
	script   string
	cluster  string
	instance string
	stdout   string
	stderr   string
	code     int
	err      error
	calls    int
}

func (f *fakeGuestOps) RunScript(_ context.Context, cluster, instance, script string) (string, string, int, error) {
	f.calls++
	f.cluster = cluster
	f.instance = instance
	f.script = script
	return f.stdout, f.stderr, f.code, f.err
}

func TestRestoreLeaseRunsRenewScriptInTargetGuest(t *testing.T) {
	ops := &fakeGuestOps{stdout: "eth0 203.0.113.50/24"}
	fixer := NewNetworkFixer(ops, testLogger())

	if err := fixer.RestoreLease(context.Background(), "c63", "web"); err != nil {
		t.Fatalf("RestoreLease() error = %v", err)
	}

	if ops.calls != 1 {
		t.Fatalf("RunScript calls = %d, want 1", ops.calls)
	}
	if ops.cluster != "c63" || ops.instance != "web" {
		t.Fatalf("ran against %s:%s, want c63:web", ops.cluster, ops.instance)
	}

	// The script must release the old lease before requesting a new one, and it
	// has to cover the DHCP clients shipped by common distributions.
	for _, want := range []string{"dhclient -r", "udhcpc", "systemd-networkd", "NetworkManager"} {
		if !strings.Contains(ops.script, want) {
			t.Errorf("renew script missing %q", want)
		}
	}
	idxRelease := strings.Index(ops.script, "dhclient -r")
	idxRequest := strings.Index(ops.script, "dhclient >/dev/null 2>&1 && renewed=1")
	if idxRelease == -1 || idxRequest == -1 {
		t.Fatalf("renew script missing release or request step: %q", ops.script)
	}
	if idxRelease > idxRequest {
		t.Error("renew script requests a lease before releasing the old one")
	}
}

func TestRestoreLeaseToleratesGuestWithoutDHCPClient(t *testing.T) {
	// A static-address guest exits non-zero; the migration must still succeed.
	ops := &fakeGuestOps{stderr: "no supported DHCP client found", code: 1}
	fixer := NewNetworkFixer(ops, testLogger())

	if err := fixer.RestoreLease(context.Background(), "c63", "static"); err != nil {
		t.Fatalf("RestoreLease() error = %v, want nil for a static guest", err)
	}
}

func TestRestoreLeasePropagatesTransportError(t *testing.T) {
	ops := &fakeGuestOps{err: errors.New("instance is stopped")}
	fixer := NewNetworkFixer(ops, testLogger())

	if err := fixer.RestoreLease(context.Background(), "c63", "web"); err == nil {
		t.Fatal("RestoreLease() expected an error when the guest cannot be reached")
	}
}

func TestRestoreLeaseNilSafety(t *testing.T) {
	// The service runs without a fixer in tests and single-cluster setups.
	var fixer *NetworkFixer
	if err := fixer.RestoreLease(context.Background(), "c63", "web"); err != nil {
		t.Fatalf("nil fixer = %v, want nil", err)
	}

	if err := NewNetworkFixer(nil, testLogger()).RestoreLease(context.Background(), "c63", "web"); err != nil {
		t.Fatalf("fixer without ops = %v, want nil", err)
	}
}

func TestGuestOpsFuncAdaptsFunction(t *testing.T) {
	var gotCluster, gotInstance, gotScript string
	fn := GuestOpsFunc(func(_ context.Context, cluster, instance, script string) (string, string, int, error) {
		gotCluster, gotInstance, gotScript = cluster, instance, script
		return "out", "err", 3, nil
	})

	stdout, stderr, code, err := fn.RunScript(context.Background(), "c1", "web", "echo hi")
	if err != nil || stdout != "out" || stderr != "err" || code != 3 {
		t.Fatalf("RunScript() = %q, %q, %d, %v", stdout, stderr, code, err)
	}
	if gotCluster != "c1" || gotInstance != "web" || gotScript != "echo hi" {
		t.Fatalf("forwarded args = %q, %q, %q", gotCluster, gotInstance, gotScript)
	}
}
