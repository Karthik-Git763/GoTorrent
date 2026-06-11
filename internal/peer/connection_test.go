package peer

import (
	"net"
	"testing"
	"time"
)

func TestNewPeerConnection(t *testing.T) {
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()

	var peerID [20]byte
	copy(peerID[:], []byte("testpeerid12345678"))

	pc := NewPeerConnection(conn1, peerID)
	if pc == nil {
		t.Fatal("NewPeerConnection returned nil")
	}
	if pc.choked.Load() != true {
		t.Fatal("expected PeerConnection to start choked")
	}
	if pc.peerID != peerID {
		t.Fatalf("peerID mismatch")
	}
}

func TestPeerConnection_StartClose(t *testing.T) {
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()

	var peerID [20]byte
	copy(peerID[:], []byte("testpeerid12345678"))

	pc := NewPeerConnection(conn1, peerID)
	pc.Start()

	// Give goroutines time to start
	time.Sleep(50 * time.Millisecond)

	// Close should not panic (idempotent via sync.Once)
	pc.Close()
	pc.Close() // second call must be safe
}

func TestPeerConnection_StartBeforeClose(t *testing.T) {
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()

	var peerID [20]byte
	copy(peerID[:], []byte("testpeerid12345678"))

	pc := NewPeerConnection(conn1, peerID)
	pc.Start()
	defer pc.Close()

	// Connection should be alive
	if pc.conn == nil {
		t.Fatal("connection is nil")
	}
}

func TestPeerConnection_ReaderReceivesMessage(t *testing.T) {
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()

	var peerID [20]byte
	copy(peerID[:], []byte("testpeerid12345678"))

	pc := NewPeerConnection(conn1, peerID)
	pc.Start()
	defer pc.Close()

	// Send a Have message through conn2 (the mock peer side)
	msg := &Message{ID: MsgHave, Payload: []byte{0, 0, 0, 42}}
	if err := WriteMessage(conn2, msg); err != nil {
		t.Fatalf("WriteMessage error: %v", err)
	}

	// Read it from the Incoming channel
	select {
	case got := <-pc.Incoming():
		if got.ID != MsgHave {
			t.Fatalf("expected MsgHave (4), got %d", got.ID)
		}
		if len(got.Payload) != 4 || got.Payload[3] != 42 {
			t.Fatalf("unexpected payload: %v", got.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message on Incoming channel")
	}
}

func TestPeerConnection_ReaderHandlesChoke(t *testing.T) {
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()

	var peerID [20]byte
	copy(peerID[:], []byte("testpeerid12345678"))

	pc := NewPeerConnection(conn1, peerID)
	pc.Start()
	defer pc.Close()

	if !pc.choked.Load() {
		t.Fatal("expected to start choked")
	}

	// Send Unchoke
	if err := WriteMessage(conn2, &Message{ID: MsgUnchoke}); err != nil {
		t.Fatalf("WriteMessage error: %v", err)
	}

	// Give the reader time to process and update state
	time.Sleep(100 * time.Millisecond)

	if pc.choked.Load() {
		t.Fatal("expected to be unchoked after receiving unchoke message")
	}

	// Send Choke
	if err := WriteMessage(conn2, &Message{ID: MsgChoke}); err != nil {
		t.Fatalf("WriteMessage error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if !pc.choked.Load() {
		t.Fatal("expected to be choked after receiving choke message")
	}
}

func TestPeerConnection_ReaderHandlesUnchoke(t *testing.T) {
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()

	var peerID [20]byte
	copy(peerID[:], []byte("testpeerid12345678"))

	pc := NewPeerConnection(conn1, peerID)
	pc.Start()
	defer pc.Close()

	if !pc.choked.Load() {
		t.Fatal("expected to start choked")
	}

	// Send Unchoke
	if err := WriteMessage(conn2, &Message{ID: MsgUnchoke}); err != nil {
		t.Fatalf("WriteMessage error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if pc.choked.Load() {
		t.Fatal("expected to be unchoked")
	}

	// Verify the message was also delivered on Incoming
	select {
	case got := <-pc.Incoming():
		if got.ID != MsgUnchoke {
			t.Fatalf("expected MsgUnchoke (1), got %d", got.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for unchoke on Incoming channel")
	}
}

func TestPeerConnection_ReaderKeepalive(t *testing.T) {
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()

	var peerID [20]byte
	copy(peerID[:], []byte("testpeerid12345678"))

	pc := NewPeerConnection(conn1, peerID)
	pc.Start()
	defer pc.Close()

	// Send keep-alive (length=0)
	keepalive := []byte{0, 0, 0, 0}
	if _, err := conn2.Write(keepalive); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// Keep-alive should be silently consumed — NOT delivered on Incoming
	// Send a real message after it to verify the reader is still alive
	time.Sleep(50 * time.Millisecond)
	if err := WriteMessage(conn2, &Message{ID: MsgHave, Payload: []byte{0, 0, 0, 1}}); err != nil {
		t.Fatalf("WriteMessage error: %v", err)
	}

	select {
	case got := <-pc.Incoming():
		if got.ID != MsgHave {
			t.Fatalf("expected MsgHave after keep-alive, got %d", got.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout — reader likely stalled after keep-alive")
	}
}

func TestPeerConnection_AssignWorkWhenChoked(t *testing.T) {
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()

	var peerID [20]byte
	copy(peerID[:], []byte("testpeerid12345678"))

	pc := NewPeerConnection(conn1, peerID)
	pc.Start()
	defer pc.Close()

	// Should not accept work when choked
	ok := pc.AssignWork(PieceWork{Index: 5, Begin: 0, Length: 16384})
	if ok {
		t.Fatal("AssignWork should return false when choked")
	}
}

func TestPeerConnection_AssignWorkWhenUnchoked(t *testing.T) {
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()

	var peerID [20]byte
	copy(peerID[:], []byte("testpeerid12345678"))

	pc := NewPeerConnection(conn1, peerID)
	pc.Start()
	defer pc.Close()

	// Send unchoke
	if err := WriteMessage(conn2, &Message{ID: MsgUnchoke}); err != nil {
		t.Fatalf("WriteMessage error: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Should accept work now
	ok := pc.AssignWork(PieceWork{Index: 5, Begin: 0, Length: 16384})
	if !ok {
		t.Fatal("AssignWork should return true when unchoked")
	}
}

func TestPeerConnection_SetAndGetBitfield(t *testing.T) {
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()

	var peerID [20]byte
	copy(peerID[:], []byte("testpeerid12345678"))

	pc := NewPeerConnection(conn1, peerID)
	pc.Start()
	defer pc.Close()

	bitfield := []bool{true, false, true, true, false}
	pc.SetBitfield(bitfield)

	got := pc.Bitfield()
	if len(got) != len(bitfield) {
		t.Fatalf("bitfield length: got %d, want %d", len(got), len(bitfield))
	}
	for i := range bitfield {
		if got[i] != bitfield[i] {
			t.Fatalf("bitfield[%d]: got %v, want %v", i, got[i], bitfield[i])
		}
	}
}

func TestPeerConnection_CloseTwice(t *testing.T) {
	conn1, conn2 := net.Pipe()
	defer conn1.Close()
	defer conn2.Close()

	var peerID [20]byte
	copy(peerID[:], []byte("testpeerid12345678"))

	pc := NewPeerConnection(conn1, peerID)
	pc.Start()

	// Close twice — must not panic
	pc.Close()
	pc.Close()
}

func TestPeerConnection_ReaderStopsOnClose(t *testing.T) {
	conn1, conn2 := net.Pipe()

	var peerID [20]byte
	copy(peerID[:], []byte("testpeerid12345678"))

	pc := NewPeerConnection(conn1, peerID)
	pc.Start()

	// Close the connection — reader should notice and exit
	conn2.Close()

	// Give reader time to detect the closed connection
	time.Sleep(200 * time.Millisecond)

	// Close should still be safe
	pc.Close()
}