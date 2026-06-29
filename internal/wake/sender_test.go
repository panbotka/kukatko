package wake

import (
	"bytes"
	"net"
	"testing"
)

// TestPacket_construction verifies the magic packet for a 6-byte MAC is the
// canonical 102 bytes: a 0xFF synchronisation stream followed by the target
// hardware address repeated 16 times.
func TestPacket_construction(t *testing.T) {
	t.Parallel()

	mac, err := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatalf("ParseMAC: %v", err)
	}

	pkt, err := Packet(mac)
	if err != nil {
		t.Fatalf("Packet: %v", err)
	}

	if len(pkt) != 102 {
		t.Fatalf("packet length = %d, want 102", len(pkt))
	}
	sync := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	if !bytes.Equal(pkt[:6], sync) {
		t.Errorf("sync stream = %x, want %x", pkt[:6], sync)
	}
	for i := range 16 {
		start := 6 + i*6
		if got := pkt[start : start+6]; !bytes.Equal(got, mac) {
			t.Errorf("repetition %d = %x, want %x", i, got, []byte(mac))
		}
	}
}

// TestPacket_invalidMAC verifies a non-6-byte hardware address is rejected
// rather than producing a malformed packet.
func TestPacket_invalidMAC(t *testing.T) {
	t.Parallel()

	// An 8-byte (EUI-64) address is not a valid Wake-on-LAN target.
	mac, err := net.ParseMAC("aa:bb:cc:dd:ee:ff:00:11")
	if err != nil {
		t.Fatalf("ParseMAC: %v", err)
	}
	if _, err := Packet(mac); err == nil {
		t.Fatal("Packet(8-byte MAC) = nil error, want error")
	}
}

// TestNewSender_udpDefault verifies that with no interface configured the
// default sender is the UDP broadcast variant.
func TestNewSender_udpDefault(t *testing.T) {
	t.Parallel()

	s, err := newSender("192.168.1.255:9", "")
	if err != nil {
		t.Fatalf("newSender: %v", err)
	}
	if _, ok := s.(udpSender); !ok {
		t.Fatalf("sender type = %T, want udpSender", s)
	}
}

// TestNewSender_unknownInterface verifies a non-existent interface name fails
// fast at construction.
func TestNewSender_unknownInterface(t *testing.T) {
	t.Parallel()

	if _, err := newSender("", "definitely-not-a-real-nic0"); err == nil {
		t.Fatal("newSender(unknown interface) = nil error, want error")
	}
}
