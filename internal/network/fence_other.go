//go:build !linux

package network

import (
	"net/netip"

	"github.com/pyvvo/funcd/api/fault"
)

// FenceNetns needs Linux network namespaces (ADR-0269).
func FenceNetns(string, []netip.AddrPort) error {
	return fault.Unavailablef("network.FenceNetns", "a netns fence requires linux")
}
