package nodenet

import (
	"context"
	"errors"
	"testing"
)

type commandStub struct {
	out string
	err error
}

func (s commandStub) Run(_ context.Context, _ string) (string, error) {
	return s.out, s.err
}

func TestBondSlaves(t *testing.T) {
	links := `[{"ifname":"bond0","link_type":"ether"},{"ifname":"eth0","master":"bond0"},{"ifname":"eth1","master":"bond0"},{"ifname":"eth2","link_type":"ether"}]`
	slaves, err := BondSlaves(context.Background(), commandStub{out: links})
	if err != nil {
		t.Fatalf("BondSlaves() error = %v", err)
	}
	if len(slaves) != 2 || slaves["eth0"] != "bond0" || slaves["eth1"] != "bond0" {
		t.Fatalf("slaves = %#v", slaves)
	}
	if _, ok := slaves["eth2"]; ok {
		t.Fatalf("non-member listed as slave: %#v", slaves)
	}
	if _, ok := slaves["bond0"]; ok {
		t.Fatalf("master itself listed as slave: %#v", slaves)
	}

	if _, err := BondSlaves(context.Background(), commandStub{err: errors.New("ssh down")}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("command error = %v", err)
	}
	if _, err := BondSlaves(context.Background(), commandStub{out: "not-json"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("parse error = %v", err)
	}
}
