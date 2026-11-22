package main

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"

	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
)

func netlinkSetLink(name string, up bool) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("%w: find %s: %v", nodenet.ErrUnavailable, name, err)
	}
	if up {
		err = netlink.LinkSetUp(link)
	} else {
		err = netlink.LinkSetDown(link)
	}
	if err != nil {
		return fmt.Errorf("%w: set %s state: %v", nodenet.ErrUnavailable, name, err)
	}
	return nil
}

func netlinkAddAddress(name, cidr string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("%w: find %s: %v", nodenet.ErrUnavailable, name, err)
	}
	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return fmt.Errorf("%w: parse %s: %v", nodenet.ErrInvalidRequest, cidr, err)
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("%w: add %s on %s: %v", nodenet.ErrUnavailable, cidr, name, err)
	}
	return nil
}

func netlinkAddDefaultRoute(name, gateway string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("%w: find %s: %v", nodenet.ErrUnavailable, name, err)
	}
	gw := net.ParseIP(gateway)
	if gw == nil {
		return fmt.Errorf("%w: gateway %q is invalid", nodenet.ErrInvalidRequest, gateway)
	}
	route := &netlink.Route{LinkIndex: link.Attrs().Index, Gw: gw, Protocol: netlink.RouteProtocol(99)}
	if err := netlink.RouteReplace(route); err != nil {
		return fmt.Errorf("%w: default route via %s: %v", nodenet.ErrUnavailable, gateway, err)
	}
	return nil
}

func netlinkAddFleetRoute(destination, gateway, device string) error {
	_, dst, err := net.ParseCIDR(destination)
	if err != nil {
		return fmt.Errorf("%w: destination %q: %v", nodenet.ErrInvalidRequest, destination, err)
	}
	route := &netlink.Route{Dst: dst, Protocol: netlink.RouteProtocol(99)}
	if gateway != "" {
		route.Gw = net.ParseIP(gateway)
		if route.Gw == nil {
			return fmt.Errorf("%w: gateway %q is invalid", nodenet.ErrInvalidRequest, gateway)
		}
	}
	if device != "" {
		link, err := netlink.LinkByName(device)
		if err != nil {
			return fmt.Errorf("%w: find %s: %v", nodenet.ErrUnavailable, device, err)
		}
		route.LinkIndex = link.Attrs().Index
	}
	if err := netlink.RouteReplace(route); err != nil {
		return fmt.Errorf("%w: add fleet route %s: %v", nodenet.ErrUnavailable, destination, err)
	}
	return nil
}

func netlinkDeleteFleetRoute(destination string) error {
	_, dst, err := net.ParseCIDR(destination)
	if err != nil {
		return fmt.Errorf("%w: destination %q: %v", nodenet.ErrInvalidRequest, destination, err)
	}
	route := &netlink.Route{Dst: dst, Protocol: netlink.RouteProtocol(99)}
	if err := netlink.RouteDel(route); err != nil {
		return fmt.Errorf("%w: delete fleet route %s: %v", nodenet.ErrUnavailable, destination, err)
	}
	return nil
}
