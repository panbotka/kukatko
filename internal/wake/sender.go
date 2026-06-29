// Package wake optionally wakes the embeddings box via Wake-on-LAN when
// image_embed/face_detect jobs are waiting and the sidecar is offline, so the
// queue can drain without manual power-on.
//
// Wake-on-LAN does not traverse Tailscale (an L3 overlay with no L2 broadcast):
// the host must share the physical LAN with the box and send the magic packet
// locally. The feature is OFF by default and fully inert when disabled — no
// queue polling, no health checks, and no packets are ever sent. Magic-packet
// construction and sending use github.com/mdlayher/wol.
package wake

import (
	"context"
	"fmt"
	"net"

	"github.com/mdlayher/wol"
)

// Packet builds the Wake-on-LAN magic packet for mac using mdlayher/wol. For a
// 6-byte MAC the result is 102 bytes: a 6-byte synchronisation stream of 0xFF
// followed by the target hardware address repeated 16 times. It is exported so
// the construction can be unit-tested without emitting any network traffic.
func Packet(mac net.HardwareAddr) ([]byte, error) {
	p := wol.MagicPacket{Target: mac}
	b, err := p.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("wake: building magic packet for %s: %w", mac, err)
	}
	return b, nil
}

// Sender sends a Wake-on-LAN magic packet targeting a MAC address. It is an
// interface so the trigger logic can be exercised with a fake that records
// calls instead of touching the network.
type Sender interface {
	// Send emits a magic packet for mac. The context bounds the send.
	Send(ctx context.Context, mac net.HardwareAddr) error
}

// udpSender broadcasts the magic packet as a UDP datagram to broadcastAddr via
// mdlayher/wol's UDP client. This is the default path and needs no elevated
// privileges; broadcastAddr should be the target subnet's broadcast address
// (for example "192.168.1.255:9").
type udpSender struct {
	broadcastAddr string
}

// Send broadcasts a magic packet for mac to the configured UDP broadcast
// address, opening and closing a short-lived socket per call.
func (s udpSender) Send(_ context.Context, mac net.HardwareAddr) error {
	client, err := wol.NewClient()
	if err != nil {
		return fmt.Errorf("wake: opening udp socket: %w", err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Wake(s.broadcastAddr, mac); err != nil {
		return fmt.Errorf("wake: sending magic packet to %s: %w", s.broadcastAddr, err)
	}
	return nil
}

// rawSender emits a raw Ethernet magic frame on a named interface via
// mdlayher/wol's raw client. It bypasses IP routing (most reliable on the local
// LAN) but requires CAP_NET_RAW.
type rawSender struct {
	iface *net.Interface
}

// Send emits a raw Ethernet magic frame for mac on the configured interface,
// opening and closing a short-lived raw socket per call.
func (s rawSender) Send(_ context.Context, mac net.HardwareAddr) error {
	client, err := wol.NewRawClient(s.iface)
	if err != nil {
		return fmt.Errorf("wake: opening raw socket on %s: %w", s.iface.Name, err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Wake(mac); err != nil {
		return fmt.Errorf("wake: sending raw magic frame on %s: %w", s.iface.Name, err)
	}
	return nil
}

// newSender builds the default network Sender: a raw-Ethernet sender when an
// interface name is configured (resolved here so a bad name fails fast), or a
// UDP broadcast sender otherwise.
func newSender(broadcastAddr, ifaceName string) (Sender, error) {
	if ifaceName != "" {
		iface, err := net.InterfaceByName(ifaceName)
		if err != nil {
			return nil, fmt.Errorf("wake: resolving interface %q: %w", ifaceName, err)
		}
		return rawSender{iface: iface}, nil
	}
	return udpSender{broadcastAddr: broadcastAddr}, nil
}
