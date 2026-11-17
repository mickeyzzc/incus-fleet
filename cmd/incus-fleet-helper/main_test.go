package main

import (
	"strings"
	"testing"
)

func TestHandleRejectsInvalidRequests(t *testing.T) {
	cases := []string{
		`not-json`,
		`{"action":"nope"}`,
		`{"action":"link-state"}`,
		`{"action":"address","interface":"eth0"}`,
		`{"action":"confirm"}`,
		`{"action":"route-add","destination":"bad","gateway":"10.0.0.1","device":"eth0"}`,
		`{"action":"incus-bootstrap"}`,
		`{"action":"incus-bootstrap","spec":"not-json"}`,
	}
	for _, body := range cases {
		resp, code := handleReader(strings.NewReader(body))
		if code == 0 || resp.OK || resp.Error == "" {
			t.Fatalf("handleReader(%q) = %#v, code %d", body, resp, code)
		}
	}
}

func TestStatePathUsesEnv(t *testing.T) {
	t.Setenv("INCUS_FLEET_HELPER_STATE_DIR", "/run/incus-fleet-helper")
	if got := statePath("task-1"); got != "/run/incus-fleet-helper/task-1.json" {
		t.Fatalf("statePath() = %q", got)
	}
	if err := removeState("missing"); err != nil {
		t.Fatalf("removeState(missing) error = %v", err)
	}
	if err := rollbackState("missing"); err == nil {
		t.Fatal("rollbackState(missing) expected error")
	}
}
