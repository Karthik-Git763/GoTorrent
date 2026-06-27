package peer

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"go-torrent/internal/torrent"
	"go-torrent/internal/tracker"
)

func TestGeneratePeerID_Length(t *testing.T) {
	id := GeneratePeerID()
	if len(id) != 20 {
		t.Fatalf("expected 20 bytes, got %d", len(id))
	}
}

func TestGeneratePeerID_Prefix(t *testing.T) {
	id := GeneratePeerID()
	prefix := string(id[:8])
	if prefix != "-GT0001-" {
		t.Fatalf("expected prefix '-GT0001-', got %q", prefix)
	}
}

func TestGeneratePeerID_Uniqueness(t *testing.T) {
	seen := make(map[[20]byte]bool)
	for range 100 {
		id := GeneratePeerID()
		if seen[id] {
			t.Fatal("duplicate peer ID generated")
		}
		seen[id] = true
	}
}

func TestGeneratePeerID_Printable(t *testing.T) {
	id := GeneratePeerID()
	for i := 8; i < 20; i++ {
		b := id[i]
		if !((b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')) {
			t.Fatalf("byte %d is not alphanumeric: %q (0x%02x)", i, b, b)
		}
	}
}

func TestCountSetBits_AllFalse(t *testing.T) {
	n := countSetBits([]bool{false, false, false})
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestCountSetBits_AllTrue(t *testing.T) {
	n := countSetBits([]bool{true, true, true, true, true})
	if n != 5 {
		t.Fatalf("expected 5, got %d", n)
	}
}

func TestCountSetBits_Mixed(t *testing.T) {
	n := countSetBits([]bool{true, false, true, true, false, false, false, true})
	if n != 4 {
		t.Fatalf("expected 4, got %d", n)
	}
}

func TestCountSetBits_Empty(t *testing.T) {
	n := countSetBits([]bool{})
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

// startFullMockPeer starts a TCP listener that responds to the full
// ConnectToPeers flow: handshake → bitfield → interested.
// Returns the address and the infoHash the mock peer expects.
func startFullMockPeer(t *testing.T) (net.Addr, [20]byte, func()) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	// Generate a deterministic infoHash for this mock peer
	var infoHash [20]byte
	copy(infoHash[:], []byte("testinfohash12345678"))

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read client handshake
		buf := make([]byte, 68)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}

		// Send valid handshake
		resp := make([]byte, 68)
		resp[0] = 19
		copy(resp[1:], []byte("BitTorrent protocol"))
		copy(resp[28:], infoHash[:])
		copy(resp[48:], []byte("mockpeeridd12345678"))
		if _, err := conn.Write(resp); err != nil {
			return
		}

		// Give client time to start goroutine pair before sending bitfield
		time.Sleep(50 * time.Millisecond)

		// Send bitfield (all pieces: 0xFF repeated)
		bitfieldLen := 4 // 32 pieces = 4 bytes
		bitfield := make([]byte, bitfieldLen)
		for i := range bitfield {
			bitfield[i] = 0xFF
		}
		if err := WriteMessage(conn, &Message{ID: MsgBitfield, Payload: bitfield}); err != nil {
			return
		}

		// Wait for Interested from client
		msg, err := ReadMessage(conn)
		if err != nil || msg == nil {
			return
		}
		_ = msg // We just verify it arrived; don't need to do anything with it
	}()

	return listener.Addr(), infoHash, func() { listener.Close() }
}

func TestConnectToPeers_SinglePeer(t *testing.T) {
	addr, infoHash, cleanup := startFullMockPeer(t)
	defer cleanup()

	torrentFile := &torrent.TorrentFile{
		InfoHash:    infoHash,
		PieceHashes: make([][20]byte, 32), // 32 pieces
		PieceLength: 16384,
		Length:      524288,
		Name:        "test.torrent",
	}

	peers := []tracker.Peer{
		{IP: net.ParseIP("127.0.0.1"), Port: uint16(addr.(*net.TCPAddr).Port)},
	}

	results := ConnectToPeers(torrentFile, peers)

	if len(results) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(results))
	}

	pc := results[0]
	if pc == nil {
		t.Fatal("got nil PeerConnection")
	}
	if pc.Bitfield() == nil {
		t.Fatal("expected bitfield to be set")
	}
	if len(pc.Bitfield()) != 32 {
		t.Fatalf("expected 32 piece bitfield, got %d", len(pc.Bitfield()))
	}
	for i, has := range pc.Bitfield() {
		if !has {
			t.Fatalf("expected all pieces set, piece %d was false", i)
		}
	}

	// Clean up
	pc.Close()
}

func TestConnectToPeersWithIDUsesAnnouncedPeerID(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	var infoHash [20]byte
	copy(infoHash[:], []byte("testinfohash12345678"))
	var announcedPeerID [20]byte
	copy(announcedPeerID[:], []byte("-GT0001-abcdefghijkl"))
	receivedPeerID := make(chan [20]byte, 1)

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()

		handshake := make([]byte, 68)
		if _, readErr := io.ReadFull(conn, handshake); readErr != nil {
			return
		}
		var got [20]byte
		copy(got[:], handshake[48:68])
		receivedPeerID <- got

		response := make([]byte, 68)
		response[0] = 19
		copy(response[1:], []byte("BitTorrent protocol"))
		copy(response[28:], infoHash[:])
		copy(response[48:], []byte("mockpeeridd12345678"))
		if _, writeErr := conn.Write(response); writeErr != nil {
			return
		}
		_ = WriteMessage(conn, &Message{ID: MsgBitfield, Payload: []byte{0x80}})
		_, _ = ReadMessage(conn)
	}()

	tf := &torrent.TorrentFile{
		InfoHash:    infoHash,
		PieceHashes: make([][20]byte, 1),
		PieceLength: 16384,
		Length:      16384,
		Name:        "test.torrent",
	}
	addr := listener.Addr().(*net.TCPAddr)
	connections, report := ConnectToPeersWithID(tf, []tracker.Peer{{IP: addr.IP, Port: uint16(addr.Port)}}, announcedPeerID)
	defer func() {
		for _, connection := range connections {
			connection.Close()
		}
	}()

	if len(connections) != 1 || report.Handshaken != 1 {
		t.Fatalf("connections = %d, handshaken = %d; want 1, 1", len(connections), report.Handshaken)
	}
	if got := <-receivedPeerID; got != announcedPeerID {
		t.Fatalf("handshake peer ID = %q, want announced ID %q", got, announcedPeerID)
	}
}

func TestConnectToPeers_SkipUnreachable(t *testing.T) {
	torrentFile := &torrent.TorrentFile{
		InfoHash:    [20]byte{},
		PieceHashes: make([][20]byte, 1),
		PieceLength: 16384,
		Length:      16384,
		Name:        "test.torrent",
	}

	// Peer on a port nothing is listening on
	peers := []tracker.Peer{
		{IP: net.ParseIP("127.0.0.1"), Port: 1},
	}

	results := ConnectToPeers(torrentFile, peers)

	if len(results) != 0 {
		t.Fatalf("expected 0 connections for unreachable peer, got %d", len(results))
	}
}

func TestConnectToPeers_MultiplePeers(t *testing.T) {
	addr1, infoHash1, cleanup1 := startFullMockPeer(t)
	defer cleanup1()

	addr2, infoHash2, cleanup2 := startFullMockPeer(t)
	defer cleanup2()

	if infoHash1 != infoHash2 {
		t.Fatal("mock peer infoHash mismatch — test design bug")
	}

	torrentFile := &torrent.TorrentFile{
		InfoHash:    infoHash1,
		PieceHashes: make([][20]byte, 32),
		PieceLength: 16384,
		Length:      524288,
		Name:        "test.torrent",
	}

	peers := []tracker.Peer{
		{IP: net.ParseIP("127.0.0.1"), Port: uint16(addr1.(*net.TCPAddr).Port)},
		{IP: net.ParseIP("127.0.0.1"), Port: uint16(addr2.(*net.TCPAddr).Port)},
	}

	results := ConnectToPeers(torrentFile, peers)

	if len(results) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(results))
	}

	for i, pc := range results {
		if pc == nil {
			t.Fatalf("connection %d is nil", i)
		}
		if len(pc.Bitfield()) != 32 {
			t.Fatalf("connection %d: expected 32 pieces, got %d", i, len(pc.Bitfield()))
		}
		pc.Close()
	}
}

func TestConnectToPeers_MixedReachability(t *testing.T) {
	addr, infoHash, cleanup := startFullMockPeer(t)
	defer cleanup()

	torrentFile := &torrent.TorrentFile{
		InfoHash:    infoHash,
		PieceHashes: make([][20]byte, 32),
		PieceLength: 16384,
		Length:      524288,
		Name:        "test.torrent",
	}

	// One reachable, one unreachable
	peers := []tracker.Peer{
		{IP: net.ParseIP("127.0.0.1"), Port: uint16(addr.(*net.TCPAddr).Port)},
		{IP: net.ParseIP("127.0.0.1"), Port: 1},
		{IP: net.ParseIP("127.0.0.1"), Port: 2},
	}

	results := ConnectToPeers(torrentFile, peers)
	_ = fmt.Sprintf("Connected to %d of %d peers", len(results), len(peers))

	// Only the reachable peer should be in results
	if len(results) != 1 {
		t.Fatalf("expected 1 connection (1 reachable, 2 unreachable), got %d", len(results))
	}
	results[0].Close()
}

func TestConnectToPeers_EmptyPeers(t *testing.T) {
	torrentFile := &torrent.TorrentFile{
		InfoHash:    [20]byte{},
		PieceHashes: make([][20]byte, 1),
		PieceLength: 16384,
		Length:      16384,
		Name:        "test.torrent",
	}

	results := ConnectToPeers(torrentFile, []tracker.Peer{})
	if len(results) != 0 {
		t.Fatalf("expected 0 connections for empty peers, got %d", len(results))
	}
}
