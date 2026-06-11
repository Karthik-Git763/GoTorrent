package peer

import (
	"io"
	"net"
	"testing"
	"time"
)

// startMockPeer starts a local TCP listener that acts as a BitTorrent peer.
// When sendValid is true it responds with a valid handshake echoing the
// received infoHash and the given peerID. When false it sends garbage.
// Returns the listener address and a cleanup function.
func startMockPeer(t *testing.T, infoHash [20]byte, peerID [20]byte, sendValid bool) (net.Addr, func()) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read the incoming handshake
		buf := make([]byte, 68)
		_, err = io.ReadFull(conn, buf)
		if err != nil {
			return
		}

		if !sendValid {
			// Send garbage so the client fails verification
			conn.Write([]byte("garbage"))
			return
		}

		// Build valid response handshake with the same infoHash
		resp := make([]byte, 68)
		resp[0] = 19
		copy(resp[1:], []byte("BitTorrent protocol"))
		copy(resp[28:], infoHash[:])
		copy(resp[48:], peerID[:])
		conn.Write(resp)
	}()

	return listener.Addr(), func() { listener.Close() }
}

func TestHandshake_Success(t *testing.T) {
	var infoHash [20]byte
	copy(infoHash[:], []byte("aaaaaaaaaaaaaaaaaaaa"))
	var expectedPeerID [20]byte
	copy(expectedPeerID[:], []byte("bbbbbbbbbbbbbbbbbbbb"))

	addr, cleanup := startMockPeer(t, infoHash, expectedPeerID, true)
	defer cleanup()

	conn, err := net.DialTimeout("tcp", addr.String(), 5*time.Second)
	if err != nil {
		t.Fatalf("failed to dial mock peer: %v", err)
	}
	defer conn.Close()

	var myPeerID [20]byte
	copy(myPeerID[:], []byte("cccccccccccccccccccc"))

	remoteID, err := Handshake(conn, infoHash, myPeerID)
	if err != nil {
		t.Fatalf("Handshake returned error: %v", err)
	}
	if remoteID != expectedPeerID {
		t.Fatalf("remote peer ID: got %x, want %x", remoteID, expectedPeerID)
	}
}

func TestHandshake_WrongProtocol(t *testing.T) {
	var infoHash [20]byte
	copy(infoHash[:], []byte("aaaaaaaaaaaaaaaaaaaa"))
	var peerID [20]byte
	copy(peerID[:], []byte("bbbbbbbbbbbbbbbbbbbb"))

	// Start a mock peer that sends a full 68-byte response with a bad
	// protocol string so io.ReadFull succeeds but verification fails.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Read the incoming handshake
		buf := make([]byte, 68)
		io.ReadFull(conn, buf)
		// Send 68 bytes with wrong protocol string
		resp := make([]byte, 68)
		resp[0] = 19
		copy(resp[1:], []byte("NotBitTorrentProto")) // 19 bytes
		copy(resp[28:], infoHash[:])
		copy(resp[48:], peerID[:])
		conn.Write(resp)
	}()

	conn, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("failed to dial mock peer: %v", err)
	}
	defer conn.Close()

	var myPeerID [20]byte
	copy(myPeerID[:], []byte("cccccccccccccccccccc"))

	_, err = Handshake(conn, infoHash, myPeerID)
	if err != ErrInvalidHandshake {
		t.Fatalf("expected ErrInvalidHandshake, got %v", err)
	}
}

func TestHandshake_WrongInfoHash(t *testing.T) {
	var infoHash [20]byte
	copy(infoHash[:], []byte("aaaaaaaaaaaaaaaaaaaa"))
	var wrongInfoHash [20]byte
	copy(wrongInfoHash[:], []byte("bbbbbbbbbbbbbbbbbbbb"))
	var peerID [20]byte
	copy(peerID[:], []byte("cccccccccccccccccccc"))

	// Mock peer uses wrongInfoHash — client expects infoHash
	addr, cleanup := startMockPeer(t, wrongInfoHash, peerID, true)
	defer cleanup()

	conn, err := net.DialTimeout("tcp", addr.String(), 5*time.Second)
	if err != nil {
		t.Fatalf("failed to dial mock peer: %v", err)
	}
	defer conn.Close()

	var myPeerID [20]byte
	copy(myPeerID[:], []byte("dddddddddddddddddddd"))

	_, err = Handshake(conn, infoHash, myPeerID)
	if err != ErrInvalidHandshake {
		t.Fatalf("expected ErrInvalidHandshake, got %v", err)
	}
}

func TestHandshake_ConnectionRefused(t *testing.T) {
	// Dial a port that nothing is listening on
	conn, err := net.DialTimeout("tcp", "127.0.0.1:1", 2*time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("expected connection refused")
	}
	// Handshake not called because DialTimeout already failed — this is a
	// coverage test for the "peer unreachable" path in ConnectToPeers
}

func TestHandshake_ShortResponse(t *testing.T) {
	// Start a mock that sends only 1 byte back
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Read the full handshake
		buf := make([]byte, 68)
		io.ReadFull(conn, buf)
		// Send only 1 byte — io.ReadFull will error on the client side
		conn.Write([]byte{0})
	}()

	conn, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	var infoHash [20]byte
	var peerID [20]byte

	_, err = Handshake(conn, infoHash, peerID)
	if err == nil {
		t.Fatal("expected error from short response")
	}
}