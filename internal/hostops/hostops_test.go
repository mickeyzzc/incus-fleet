package hostops

import (
	"testing"

	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
)

var _ Operator = (*nodenet.Service)(nil)

func TestParseMode(t *testing.T) {
	cases := []struct {
		value string
		want  Mode
		err   bool
	}{
		{"", ModeAuto, false},
		{"auto", ModeAuto, false},
		{" AUTO ", ModeAuto, false},
		{"agent", ModeAgent, false},
		{"ssh", ModeSSH, false},
		{"local", ModeLocal, false},
		{"magic", "", true},
	}
	for _, tc := range cases {
		got, err := ParseMode(tc.value)
		if tc.err {
			if err == nil {
				t.Fatalf("ParseMode(%q) expected error", tc.value)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("ParseMode(%q) = %q, %v; want %q", tc.value, got, err, tc.want)
		}
	}
}

func TestModeString(t *testing.T) {
	if ModeAuto.String() != "auto" || ModeAgent.String() != "agent" ||
		ModeSSH.String() != "ssh" || ModeLocal.String() != "local" {
		t.Fatal("Mode.String() returned an unexpected value")
	}
}
