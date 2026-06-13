package piece

import (
	"testing"

	"go-torrent/internal/peer"
)

func makePeerWithBitfield(bitfield []bool) *peer.PeerConnection {
	return newMockPeer(bitfield)
}

func newMockPeer(bitfield []bool) *peer.PeerConnection {
	// We can't easily create a PeerConnection without a real net.Conn,
	// so we use a minimal approach: create one knowing Start/Close won't be called.
	// Instead we just SetBitfield and test the selection logic.
	pc := &peer.PeerConnection{}
	pc.SetBitfield(bitfield)
	return pc
}

// RarestPiece Tests

func TestRarestPiece_SelectsLeastAvailable(t *testing.T) {
	// 5 pieces, 3 peers with different availability
	peers := []*peer.PeerConnection{
		makePeerWithBitfield([]bool{true, true, true, false, false}),  // has 0,1,2
		makePeerWithBitfield([]bool{true, true, false, true, false}),  // has 0,1,3
		makePeerWithBitfield([]bool{true, false, true, false, false}), // has 0,2
	}

	m := &Manager{
		totalPieces:   5,
		peers:         peers,
		completed:     make([]bool, 5),
		inProgress:    make(map[uint32]bool),
	}

	// Peer 0 has pieces 0,1,2
	// Availability: piece0=3, piece1=2, piece2=2, piece3=1, piece4=0
	// Peer 0 can request: 0,1,2. Rarest among those peer0 has: piece1 or piece2 (both avail=2)
	// Piece0 has avail=3. So rarest peer0 can request is 1 or 2 (both avail=2)
	idx := m.RarestPiece(peers[0])
	if idx != 1 && idx != 2 {
		t.Fatalf("expected rarest piece for peer0 to be 1 or 2 (avail=2), got %d", idx)
	}

	// Peer 2 has pieces 0,2
	// Among those: piece0 avail=3, piece2 avail=2
	// Rarest: piece2
	idx = m.RarestPiece(peers[2])
	if idx != 2 {
		t.Fatalf("expected rarest piece for peer2 to be 2 (avail=2), got %d", idx)
	}
}

func TestRarestPiece_SkipsCompleted(t *testing.T) {
	peers := []*peer.PeerConnection{
		makePeerWithBitfield([]bool{true, true, true}),
		makePeerWithBitfield([]bool{true, true, false}),
	}

	m := &Manager{
		totalPieces: 3,
		peers:       peers,
		completed:   []bool{false, true, false},
		inProgress:  make(map[uint32]bool),
	}

	// Piece 1 is completed, should be skipped
	// Peer0 has pieces 0,1,2. Skipping 1: only 0 and 2 available.
	// Availability: piece0=2, piece2=1. Rarest: piece2
	idx := m.RarestPiece(peers[0])
	if idx != 2 {
		t.Fatalf("expected rarest piece (skipping completed) to be 2, got %d", idx)
	}
}

func TestRarestPiece_SkipsInProgress(t *testing.T) {
	peers := []*peer.PeerConnection{
		makePeerWithBitfield([]bool{true, true, true}),
		makePeerWithBitfield([]bool{true, false, true}),
	}

	m := &Manager{
		totalPieces: 3,
		peers:       peers,
		completed:   make([]bool, 3),
		inProgress:  map[uint32]bool{1: true},
	}

	// Piece 1 is in progress, should be skipped
	// Peer0 has all 3, but only 0 and 2 are available
	// Availability: piece0=2, piece2=2. Both equal, rarest is a random pick.
	idx := m.RarestPiece(peers[0])
	if idx != 0 && idx != 2 {
		t.Fatalf("expected rarest piece (skipping in-progress) to be 0 or 2, got %d", idx)
	}
}

func TestRarestPiece_SkipsPeerMissingPieces(t *testing.T) {
	peers := []*peer.PeerConnection{
		makePeerWithBitfield([]bool{true, false, false, false}), // peer0 only has piece 0
		makePeerWithBitfield([]bool{true, true, true, true}),    // peer1 has all
	}

	m := &Manager{
		totalPieces: 4,
		peers:       peers,
		completed:   make([]bool, 4),
		inProgress:  make(map[uint32]bool),
	}

	// For peer0, only piece 0 is available. Must return 0.
	idx := m.RarestPiece(peers[0])
	if idx != 0 {
		t.Fatalf("expected peer0's only available piece to be 0, got %d", idx)
	}
}

func TestRarestPiece_NoAvailablePieces(t *testing.T) {
	peers := []*peer.PeerConnection{
		makePeerWithBitfield([]bool{false, false}),
	}

	m := &Manager{
		totalPieces: 2,
		peers:       peers,
		completed:   make([]bool, 2),
		inProgress:  make(map[uint32]bool),
	}

	idx := m.RarestPiece(peers[0])
	if idx != -1 {
		t.Fatalf("expected -1 when no pieces available, got %d", idx)
	}
}

func TestRarestPiece_AllCompleted(t *testing.T) {
	peers := []*peer.PeerConnection{
		makePeerWithBitfield([]bool{true, true}),
	}

	m := &Manager{
		totalPieces: 2,
		peers:       peers,
		completed:   []bool{true, true},
		inProgress:  make(map[uint32]bool),
	}

	idx := m.RarestPiece(peers[0])
	if idx != -1 {
		t.Fatalf("expected -1 when all pieces completed, got %d", idx)
	}
}

// FirstPiece Tests

func TestFirstPiece_ReturnsFirstAvailable(t *testing.T) {
	m := &Manager{
		totalPieces: 5,
		completed:   make([]bool, 5),
	}

	peer := makePeerWithBitfield([]bool{false, false, true, true, false})

	idx := m.FirstPiece(peer)
	if idx != 2 {
		t.Fatalf("expected first available piece to be 2, got %d", idx)
	}
}

func TestFirstPiece_SkipsCompleted(t *testing.T) {
	m := &Manager{
		totalPieces: 4,
		completed:   []bool{false, true, false, false},
	}

	peer := makePeerWithBitfield([]bool{true, true, true, true})

	// Piece 1 is completed, should skip it
	idx := m.FirstPiece(peer)
	if idx != 0 {
		t.Fatalf("expected first available piece (skipping completed) to be 0, got %d", idx)
	}
}

func TestFirstPiece_NoAvailablePieces(t *testing.T) {
	m := &Manager{
		totalPieces: 3,
		completed:   make([]bool, 3),
	}

	peer := makePeerWithBitfield([]bool{false, false, false})

	idx := m.FirstPiece(peer)
	if idx != -1 {
		t.Fatalf("expected -1 when peer has no pieces, got %d", idx)
	}
}

func TestFirstPiece_EmptyBitfield(t *testing.T) {
	m := &Manager{
		totalPieces: 3,
		completed:   make([]bool, 3),
	}

	peer := makePeerWithBitfield(nil)

	idx := m.FirstPiece(peer)
	if idx != -1 {
		t.Fatalf("expected -1 when peer has nil bitfield, got %d", idx)
	}
}

// RarestPiece Tiebreaker Tests

func TestRarestPiece_RandomTiebreaker(t *testing.T) {
	// All 3 peers have the same 3 pieces
	// All pieces have availability=3, so rarest-first should pick randomly
	// We run multiple times and verify we see variety (not always the same index)
	peers := []*peer.PeerConnection{
		makePeerWithBitfield([]bool{true, true, true}),
		makePeerWithBitfield([]bool{true, true, true}),
		makePeerWithBitfield([]bool{true, true, true}),
	}

	m := &Manager{
		totalPieces: 3,
		peers:       peers,
		completed:   make([]bool, 3),
		inProgress:  make(map[uint32]bool),
	}

	// Run many times and verify we get variety
	seen := make(map[int]int)
	for range 100 {
		idx := m.RarestPiece(peers[0])
		if idx < 0 || idx > 2 {
			t.Fatalf("expected index in [0,2], got %d", idx)
		}
		seen[idx]++
	}

	// Each piece should be selected at least once in 100 tries
	for i := range 3 {
		if seen[i] == 0 {
			t.Fatalf("piece %d was never selected in 100 tries — tiebreaker not random", i)
		}
	}
}

func TestRarestPiece_TiebreakerAmongEqual(t *testing.T) {
	// Piece 0: avail=2, Piece 1: avail=2, Piece 2: avail=1
	// Peer0 has all 3. Rarest should be piece 2 (only one at avail=1)
	peers := []*peer.PeerConnection{
		makePeerWithBitfield([]bool{true, true, true}),
		makePeerWithBitfield([]bool{true, false, false}),
		makePeerWithBitfield([]bool{false, true, false}),
	}

	m := &Manager{
		totalPieces: 3,
		peers:       peers,
		completed:   make([]bool, 3),
		inProgress:  make(map[uint32]bool),
	}

	for range 50 {
		idx := m.RarestPiece(peers[0])
		if idx != 2 {
			t.Fatalf("expected rarest piece to be 2 (avail=1), got %d", idx)
		}
	}
}

// Test that the selection is deterministic modulo the random tiebreaker
func TestRarestPiece_SelectsFromThisPeerOnly(t *testing.T) {
	// Peer0 has piece 0 and 3 only
	// Peer1 has piece 1 and 2 only
	peers := []*peer.PeerConnection{
		makePeerWithBitfield([]bool{true, false, false, true}),
		makePeerWithBitfield([]bool{false, true, true, false}),
	}

	m := &Manager{
		totalPieces: 4,
		peers:       peers,
		completed:   make([]bool, 4),
		inProgress:  make(map[uint32]bool),
	}

	// Peer0 only has pieces 0 and 3
	// Availability: piece0=1, piece3=1 — tiebreaker
	idx := m.RarestPiece(peers[0])
	if idx != 0 && idx != 3 {
		t.Fatalf("expected peer0 to get piece 0 or 3, got %d", idx)
	}

	// Peer1 only has pieces 1 and 2
	idx = m.RarestPiece(peers[1])
	if idx != 1 && idx != 2 {
		t.Fatalf("expected peer1 to get piece 1 or 2, got %d", idx)
	}
}