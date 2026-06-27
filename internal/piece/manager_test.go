package piece

import (
	"context"
	"crypto/sha1"
	"io"
	"os"
	"path/filepath"
	"testing"

	"go-torrent/internal/peer"
	"go-torrent/internal/torrent"
	"go-torrent/internal/webseed"
)

type fakeWebSeed struct {
	pieces map[uint32][]byte
}

func (f *fakeWebSeed) Name() string { return "fake" }
func (f *fakeWebSeed) FetchPiece(ctx context.Context, index uint32, length int64) ([]byte, error) {
	data := f.pieces[index]
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}
func (f *fakeWebSeed) Disable()        {}
func (f *fakeWebSeed) Available() bool { return true }

// VerifyPiece Tests

func TestVerifyPiece_Success(t *testing.T) {
	expectedData := []byte("hello world, this is test piece data for verification")
	hash := sha1.Sum(expectedData)

	m := &Manager{
		pieceHashes: [][20]byte{hash},
	}

	if !m.verifyPiece(0, expectedData) {
		t.Fatal("expected piece verification to pass")
	}
}

func TestVerifyPiece_Failure(t *testing.T) {
	expectedData := []byte("correct piece data")
	wrongData := []byte("corrupted piece data")
	hash := sha1.Sum(expectedData)

	m := &Manager{
		pieceHashes: [][20]byte{hash},
	}

	if m.verifyPiece(0, wrongData) {
		t.Fatal("expected piece verification to fail with wrong data")
	}
}

func TestVerifyPiece_MultiplePieces(t *testing.T) {
	data0 := []byte("piece zero data")
	data1 := []byte("piece one data")
	hash0 := sha1.Sum(data0)
	hash1 := sha1.Sum(data1)

	m := &Manager{
		pieceHashes: [][20]byte{hash0, hash1},
	}

	if !m.verifyPiece(0, data0) {
		t.Fatal("piece 0 should verify correctly")
	}
	if !m.verifyPiece(1, data1) {
		t.Fatal("piece 1 should verify correctly")
	}
	if m.verifyPiece(0, data1) {
		t.Fatal("piece 0 should NOT match data1's hash")
	}
}

func TestDownloadWebSeedOnly(t *testing.T) {
	piece0 := []byte("abcd")
	piece1 := []byte("ef")
	hash0 := sha1.Sum(piece0)
	hash1 := sha1.Sum(piece1)

	tf := &torrent.TorrentFile{
		Name:        "out.bin",
		Length:      6,
		PieceLength: 4,
		PieceHashes: [][20]byte{hash0, hash1},
	}
	m := NewManager(tf, nil)
	m.webseeds = []webseed.Source{&fakeWebSeed{pieces: map[uint32][]byte{
		0: piece0,
		1: piece1,
	}}}
	m.SetLogWriter(io.Discard)

	dir := t.TempDir()
	if err := m.Download(dir, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "out.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abcdef" {
		t.Fatalf("file = %q, want abcdef", data)
	}
}

// nextPiece Tests

func TestNextPiece_SelectsRarest(t *testing.T) {
	// 5 pieces, 3 peers with different availability
	peers := []*peer.PeerConnection{
		makePeerWithBitfield([]bool{true, true, true, false, false}),  // has 0,1,2
		makePeerWithBitfield([]bool{true, true, false, true, false}),  // has 0,1,3
		makePeerWithBitfield([]bool{true, false, true, false, false}), // has 0,2
	}

	m := &Manager{
		totalPieces: 5,
		peers:       peers,
		completed:   make([]bool, 5),
		inProgress:  make(map[uint32]bool),
	}

	// Peer 0 has pieces 0,1,2. Availability: piece0=3, piece1=2, piece2=2.
	// Rarest among what peer0 has: piece1 or piece2 (avail=2).
	idx := m.nextPiece(peers[0])
	if idx != 1 && idx != 2 {
		t.Fatalf("expected nextPiece for peer0 to be 1 or 2 (avail=2), got %d", idx)
	}
}

func TestNextPiece_SkipsCompleted(t *testing.T) {
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

	// Piece 1 is completed, should be skipped.
	// Peer0 has pieces 0,1,2. Skipping 1: piece0 avail=2, piece2 avail=1.
	// Rarest: piece2.
	idx := m.nextPiece(peers[0])
	if idx != 2 {
		t.Fatalf("expected nextPiece (skipping completed) to be 2, got %d", idx)
	}
}

func TestNextPiece_SkipsInProgressForRarest(t *testing.T) {
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

	// RarestPiece skips inProgress (piece 1), so only 0 and 2 are eligible.
	// Availability: piece0=2, piece2=2. Both equal, pick randomly.
	idx := m.nextPiece(peers[0])
	if idx != 0 && idx != 2 {
		t.Fatalf("expected nextPiece (skipping in-progress) to be 0 or 2, got %d", idx)
	}
}

func TestNextPiece_FallsBackToFirstPiece(t *testing.T) {
	// All pieces the peer has are either completed or inProgress.
	// RarestPiece returns -1, so nextPiece falls back to FirstPiece,
	// which ignores inProgress — acting as endgame.
	peers := []*peer.PeerConnection{
		makePeerWithBitfield([]bool{true, true, true}),
		makePeerWithBitfield([]bool{true, true, false}),
	}

	m := &Manager{
		totalPieces: 3,
		peers:       peers,
		completed:   []bool{false, false, true},
		inProgress:  map[uint32]bool{0: true, 1: true},
	}

	// Peer0 has all 3 pieces. Pieces 0 and 1 are inProgress, piece 2 is completed.
	// RarestPiece skips completed and inProgress → -1 (no eligible pieces).
	// Falls back to FirstPiece: skips only completed (piece 2) → returns 0.
	idx := m.nextPiece(peers[0])
	if idx != 0 {
		t.Fatalf("expected nextPiece to fall back to FirstPiece returning 0, got %d", idx)
	}
}

func TestNextPiece_NoAvailablePieces(t *testing.T) {
	peers := []*peer.PeerConnection{
		makePeerWithBitfield([]bool{false, false}),
	}

	m := &Manager{
		totalPieces: 2,
		peers:       peers,
		completed:   make([]bool, 2),
		inProgress:  make(map[uint32]bool),
	}

	idx := m.nextPiece(peers[0])
	if idx != -1 {
		t.Fatalf("expected -1 when no pieces available, got %d", idx)
	}
}

func TestNextPiece_AllCompleted(t *testing.T) {
	peers := []*peer.PeerConnection{
		makePeerWithBitfield([]bool{true, true}),
	}

	m := &Manager{
		totalPieces: 2,
		peers:       peers,
		completed:   []bool{true, true},
		inProgress:  make(map[uint32]bool),
	}

	idx := m.nextPiece(peers[0])
	if idx != -1 {
		t.Fatalf("expected -1 when all pieces completed, got %d", idx)
	}
}

func TestNextPiece_EndgameSelectsInProgress(t *testing.T) {
	// In endgame mode (all fresh pieces claimed), FirstPiece fallback
	// allows selecting pieces that are inProgress.
	peers := []*peer.PeerConnection{
		makePeerWithBitfield([]bool{true, true, true}),
		makePeerWithBitfield([]bool{true, true, true}),
	}

	m := &Manager{
		totalPieces: 3,
		peers:       peers,
		completed:   []bool{false, false, false},
		inProgress:  map[uint32]bool{0: true, 1: true},
	}

	// Piece 2 is the only fresh one — RarestPiece should pick it.
	// After piece 2 is claimed, next round: 0 and 1 are inProgress,
	// RarestPiece returns -1, falls back to FirstPiece → 0.
	idx1 := m.nextPiece(peers[0])
	if idx1 != 2 {
		t.Fatalf("expected first selection to be piece 2 (only fresh one), got %d", idx1)
	}

	// Mark piece 2 as inProgress too, simulating it was claimed.
	m.inProgress[2] = true

	// Now everything is inProgress — FirstPiece fallback should pick 0.
	idx2 := m.nextPiece(peers[0])
	if idx2 != 0 {
		t.Fatalf("expected endgame fallback to return 0, got %d", idx2)
	}
}

// InProgress/Completed Helpers

func TestMarkers(t *testing.T) {
	m := &Manager{
		totalPieces: 3,
		completed:   make([]bool, 3),
		inProgress:  make(map[uint32]bool),
	}

	if m.isCompleted(0) {
		t.Fatal("piece 0 should not be completed initially")
	}

	m.markCompleted(0)
	if !m.isCompleted(0) {
		t.Fatal("piece 0 should be completed after marking")
	}
	if m.isCompleted(2) {
		t.Fatal("piece 2 should still not be completed")
	}

	if m.isInProgress(1) {
		t.Fatal("piece 1 should not be in progress initially")
	}
	m.markInProgress(1)
	if !m.isInProgress(1) {
		t.Fatal("piece 1 should be in progress after marking")
	}
	m.markNotInProgress(1)
	if m.isInProgress(1) {
		t.Fatal("piece 1 should not be in progress after clearing")
	}
}
