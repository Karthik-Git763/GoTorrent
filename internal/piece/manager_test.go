package piece

import (
	"bytes"
	"crypto/sha1"
	"testing"

	"go-torrent/internal/peer"
)

// PieceBuffer Tests

func TestNewPieceBuffer_NormalPiece(t *testing.T) {
	pb := newPieceBuffer(1 << 15) // 32KB piece
	expectedBlocks := 2           // 32KB / 16KB = 2

	if len(pb.data) != 1<<15 {
		t.Fatalf("expected data len %d, got %d", 1<<15, len(pb.data))
	}
	if pb.totalBlocks != expectedBlocks {
		t.Fatalf("expected %d total blocks, got %d", expectedBlocks, pb.totalBlocks)
	}
	if pb.receivedBlocks != 0 {
		t.Fatalf("expected 0 received blocks, got %d", pb.receivedBlocks)
	}
}

func TestNewPieceBuffer_LastPieceSmall(t *testing.T) {
	pb := newPieceBuffer(5000) // smaller than blockSize
	if len(pb.data) != 5000 {
		t.Fatalf("expected data len 5000, got %d", len(pb.data))
	}
	if pb.totalBlocks != 1 {
		t.Fatalf("expected 1 total block, got %d", pb.totalBlocks)
	}
}

func TestNewPieceBuffer_ExactBlockSize(t *testing.T) {
	pb := newPieceBuffer(blockSize) // exactly 16KB
	if pb.totalBlocks != 1 {
		t.Fatalf("expected 1 total block for exact blockSize, got %d", pb.totalBlocks)
	}
}

// ProduceWork Tests
func TestProduceWork_SinglePiece(t *testing.T) {
	m := &Manager{
		totalPieces:  1,
		pieceLength:  1 << 15, // 32KB
		totalLength:  1 << 15,
		pieceHashes:  make([][20]byte, 1),
		completed:    make([]bool, 1),
		pendingPieces: make(map[uint32]*PieceBuffer),
		inProgress:   make(map[uint32]bool),
	}

	workQueue := make(chan *PieceWork, 100)
	go m.produceWork(workQueue)

	var items []*PieceWork
	for item := range workQueue {
		items = append(items, item)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 work items (blocks) for 32KB piece, got %d", len(items))
	}

	// First block: offset 0, length 16384
	if items[0].Index != 0 || items[0].Begin != 0 || items[0].Length != blockSize {
		t.Fatalf("first block: expected Index=0 Begin=0 Length=%d, got Index=%d Begin=%d Length=%d",
			blockSize, items[0].Index, items[0].Begin, items[0].Length)
	}

	// Second block: offset 16384, length 16384 (remaining)
	if items[1].Index != 0 || items[1].Begin != blockSize || items[1].Length != blockSize {
		t.Fatalf("second block: expected Index=0 Begin=%d Length=%d, got Index=%d Begin=%d Length=%d",
			blockSize, blockSize, items[1].Index, items[1].Begin, items[1].Length)
	}
}

func TestProduceWork_MultiplePieces(t *testing.T) {
	m := &Manager{
		totalPieces:  3,
		pieceLength:  blockSize, // each piece is exactly 1 block
		totalLength:  3 * blockSize,
		pieceHashes:  make([][20]byte, 3),
		completed:    make([]bool, 3),
		pendingPieces: make(map[uint32]*PieceBuffer),
		inProgress:   make(map[uint32]bool),
	}

	workQueue := make(chan *PieceWork, 100)
	go m.produceWork(workQueue)

	var items []*PieceWork
	for item := range workQueue {
		items = append(items, item)
	}

	if len(items) != 3 {
		t.Fatalf("expected 3 work items, got %d", len(items))
	}

	for i, item := range items {
		if item.Index != uint32(i) {
			t.Fatalf("item %d: expected Index=%d, got %d", i, i, item.Index)
		}
		if item.Length != blockSize {
			t.Fatalf("item %d: expected Length=%d, got %d", i, blockSize, item.Length)
		}
	}
}

func TestProduceWork_LastPiecePartial(t *testing.T) {
	lastPieceSize := uint64(5000)
	m := &Manager{
		totalPieces:  2,
		pieceLength:  blockSize,
		totalLength:  blockSize + lastPieceSize,
		pieceHashes:  make([][20]byte, 2),
		completed:    make([]bool, 2),
		pendingPieces: make(map[uint32]*PieceBuffer),
		inProgress:   make(map[uint32]bool),
	}

	workQueue := make(chan *PieceWork, 100)
	go m.produceWork(workQueue)

	var items []*PieceWork
	for item := range workQueue {
		items = append(items, item)
	}

	// Piece 0: 1 block (exactly blockSize)
	// Piece 1: 1 block (5000 bytes, partial)
	if len(items) != 2 {
		t.Fatalf("expected 2 work items, got %d", len(items))
	}

	if items[0].Length != blockSize {
		t.Fatalf("piece 0: expected Length=%d, got %d", blockSize, items[0].Length)
	}
	if items[1].Length != uint32(lastPieceSize) {
		t.Fatalf("piece 1 (last): expected Length=%d, got %d", lastPieceSize, items[1].Length)
	}
}

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

// AssembleBlock Tests
func TestAssembleBlock_CompletesPiece(t *testing.T) {
	pieceData := []byte("this is a 32KB block of test data. it's short for test")[:32]
	// Pad to make it exactly blockSize * 2 for proper test behavior
	pieceData = append(pieceData, bytes.Repeat([]byte{0xAB}, 2*blockSize-len(pieceData))...)[:2*blockSize]

	// Produce a full 2-block piece (32KB)
	// Actually let's simplify — use a small piece size for testing
	pieceSize := uint64(2 * blockSize)

	m := &Manager{
		totalPieces:   1,
		pieceLength:   pieceSize,
		totalLength:   pieceSize,
		pieceHashes:   [][20]byte{sha1.Sum(pieceData)},
		completed:     make([]bool, 1),
		pendingPieces: make(map[uint32]*PieceBuffer),
		inProgress:    make(map[uint32]bool),
		pieceResults:  make(chan PieceResult, 10),
	}

	// Send first block
	msg0 := makeBlockMessage(0, 0, blockSize, pieceData[:blockSize])
	m.assembleBlock(msg0)

	if m.completed[0] {
		t.Fatal("piece should NOT be completed after only 1 block")
	}

	if _, exists := m.pendingPieces[0]; !exists {
		t.Fatal("pendingPieces should have piece 0 after first block")
	}

	// Send second block
	msg1 := makeBlockMessage(0, blockSize, blockSize, pieceData[blockSize:])
	m.assembleBlock(msg1)

	if !m.completed[0] {
		t.Fatal("piece 0 should be completed after all blocks")
	}

	// Check the result was sent to pieceResults
	select {
	case result := <-m.pieceResults:
		if result.Index != 0 {
			t.Fatalf("expected result index 0, got %d", result.Index)
		}
		if result.Err != nil {
			t.Fatalf("expected no error, got %v", result.Err)
		}
		if !bytes.Equal(result.Data, pieceData) {
			t.Fatal("assembled piece data does not match original")
		}
	default:
		t.Fatal("expected a PieceResult on the channel")
	}
}

func TestAssembleBlock_CorruptPiece(t *testing.T) {
	correctData := []byte("correct piece data that will be verified")
	wrongData := []byte("wrong piece data that wont match the hash")
	hash := sha1.Sum(correctData)

	m := &Manager{
		totalPieces:   1,
		pieceLength:   uint64(len(correctData)),
		totalLength:   uint64(len(correctData)),
		pieceHashes:   [][20]byte{hash},
		completed:     make([]bool, 1),
		peers:         []*peer.PeerConnection{nil}, // placeholder
		pendingPieces: make(map[uint32]*PieceBuffer),
		inProgress:    make(map[uint32]bool),
		pieceResults:  make(chan PieceResult, 10),
	}

	// Mark as in progress so handleCorruptPiece can clear it
	m.inProgress[0] = true

	// Send the wrong data (last piece, single block since smaller than blockSize)
	msg := makeBlockMessage(0, 0, uint32(len(wrongData)), wrongData)
	m.assembleBlock(msg)

	if m.completed[0] {
		t.Fatal("piece should NOT be completed with corrupt data")
	}

	// Should have been cleared from inProgress
	if m.inProgress[0] {
		t.Fatal("corrupt piece should be removed from inProgress")
	}
}

// InProgress/Completed Helpers
func TestMarkers(t *testing.T) {
	m := &Manager{
		totalPieces:  3,
		completed:    make([]bool, 3),
		inProgress:   make(map[uint32]bool),
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

// Helper

// makeBlockMessage creates a peer.Message that looks like a 'piece' message
// with the given index, begin offset, and block data.
func makeBlockMessage(index, begin, length uint32, data []byte) *peer.Message {
	payload := make([]byte, 8+len(data))
	payload[0] = byte(index >> 24)
	payload[1] = byte(index >> 16)
	payload[2] = byte(index >> 8)
	payload[3] = byte(index)
	payload[4] = byte(begin >> 24)
	payload[5] = byte(begin >> 16)
	payload[6] = byte(begin >> 8)
	payload[7] = byte(begin)
	copy(payload[8:], data[:length])
	return &peer.Message{
		ID:      peer.MsgPiece,
		Payload: payload,
	}
}