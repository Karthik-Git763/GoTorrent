package piece

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"sync"

	"go-torrent/internal/peer"
	"go-torrent/internal/torrent"
)

const blockSize = 16 * 1024 // 16KB

// PieceWork represents a single block to request from a peer.
type PieceWork struct {
	Index  uint32 // piece index
	Begin  uint32 // offset within the piece
	Length uint32 // block size
}

// PieceResult is the outcome of downloading a complete piece.
type PieceResult struct {
	Index uint32
	Data  []byte
	Err   error
}

// PieceBuffer holds in-progress assembly of a single piece's blocks.
type PieceBuffer struct {
	index          uint32
	data           []byte
	receivedBlocks int
	totalBlocks    int
}

// Manager orchestrates piece downloading across multiple peers.
type Manager struct {
	totalPieces  int
	pieceLength  uint64
	totalLength  uint64
	pieceHashes  [][20]byte
	peers        []*peer.PeerConnection
	completed    []bool
	pieceResults chan PieceResult // workers -> manager collector

	mu            sync.Mutex
	pendingPieces map[uint32]*PieceBuffer
	inProgress    map[uint32]bool
}

// NewManager creates a Manager ready to start downloading.
// Only sets up metadata; call Download to run the full pipeline.
func NewManager(torrent *torrent.TorrentFile, peers []*peer.PeerConnection) *Manager {
	return &Manager{
		totalPieces:   len(torrent.PieceHashes),
		pieceLength:   uint64(torrent.PieceLength),
		totalLength:   uint64(torrent.Length),
		pieceHashes:   torrent.PieceHashes,
		peers:         peers,
		completed:     make([]bool, len(torrent.PieceHashes)),
		pieceResults:  make(chan PieceResult, 32),
		pendingPieces: make(map[uint32]*PieceBuffer),
		inProgress:    make(map[uint32]bool),
	}
}

// Download runs the full download pipeline:
//  1. generate all block work items
//  2. launch one worker goroutine per connected peer
//  3. collect and assemble piece results
//  4. verify each piece SHA1 hash
//  5. write verified pieces to file
//
// Blocks until all pieces are downloaded or an error occurs.
func (m *Manager) Download(outputPath string, infoHash [20]byte, peerID [20]byte, port uint16) error {
	if len(m.peers) == 0 {
		return fmt.Errorf("no peers available")
	}

	workQueue := make(chan *PieceWork, m.totalPieces*16)

	// Producer: generate all block work items
	go m.produceWork(workQueue)

	// Consumers: one worker per peer
	var wg sync.WaitGroup
	for _, p := range m.peers {
		wg.Go(func() {
			m.worker(workQueue, p)
		})
	}

	// Close work queue when all producers are done
	// (workQueue is consumed by workers; we close when consumers finish)
	go func() {
		wg.Wait()
		close(m.pieceResults)
	}()

	// Collector: read piece results and write to file
	completedPieces := 0
	for result := range m.pieceResults {
		if result.Err != nil {
			return result.Err
		}
		if result.Index >= uint32(len(m.completed)) || m.completed[result.Index] {
			continue
		}
		m.completed[result.Index] = true
		completedPieces++
		fmt.Printf("Progress %d/%d pieces (%.1f%%)\n",
			completedPieces, m.totalPieces,
			float64(completedPieces)/float64(m.totalPieces)*100)
	}

	return nil
}

// produceWork generates all block-level work items and sends them to workQueue.
// The last piece may be smaller than pieceLength.
func (m *Manager) produceWork(workQueue chan<- *PieceWork) {
	defer close(workQueue)
	for i := 0; i < m.totalPieces; i++ {
		pieceSize := m.pieceLength
		if i == m.totalPieces-1 {
			pieceSize = m.totalLength - uint64(i)*m.pieceLength
		}
		numBlocks := int((pieceSize + blockSize - 1) / blockSize)
		for b := range numBlocks {
			begin := int64(b) * blockSize
			length := int64(blockSize)
			if begin+length > int64(pieceSize) {
				length = int64(pieceSize) - begin
			}
			workQueue <- &PieceWork{
				Index:  uint32(i),
				Begin:  uint32(begin),
				Length: uint32(length),
			}
		}
	}
}

// worker pulls work from the shared queue and requests blocks from its peer.
// It listens on the peer's Incoming channel for piece responses and feeds
// them into the assembly pipeline.
func (m *Manager) worker(workQueue <-chan *PieceWork, p *peer.PeerConnection) {
	for work := range workQueue {
		m.markInProgress(work.Index)

		ok := p.AssignWork(peer.PieceWork{
			Index:  int(work.Index),
			Begin:  int(work.Begin),
			Length: int(work.Length),
		})
		if !ok {
			m.markNotInProgress(work.Index)
			continue
		}

		// Wait for the corresponding piece message from this peer.
		// We drain messages on Incoming() until we get a MsgPiece.
		for msg := range p.Incoming() {
			if msg.ID == peer.MsgPiece {
				m.assembleBlock(msg)
				break
			}
			// Track have messages to keep bitfield current.
			if msg.ID == peer.MsgHave && len(msg.Payload) == 4 {
				idx := binary.BigEndian.Uint32(msg.Payload)
				bf := p.Bitfield()
				if int(idx) < len(bf) {
					bf[idx] = true
				}
			}
		}
	}
}

// assembleBlock stores a received block and, when the piece is complete,
// verifies its SHA1 hash and sends the result.
func (m *Manager) assembleBlock(msg *peer.Message) {
	// Payload: [4 bytes index][4 bytes begin][N bytes data]
	index := binary.BigEndian.Uint32(msg.Payload[0:4])
	begin := binary.BigEndian.Uint32(msg.Payload[4:8])
	data := msg.Payload[8:]

	m.mu.Lock()
	defer m.mu.Unlock()

	piece := m.pendingPieces[index]
	if piece == nil {
		pieceSize := m.pieceLength
		if int(index) == m.totalPieces-1 {
			pieceSize = m.totalLength - uint64(index)*m.pieceLength
		}
		piece = newPieceBuffer(pieceSize)
		piece.index = index
		m.pendingPieces[index] = piece
	}

	copy(piece.data[begin:], data)
	piece.receivedBlocks++

	// Check if the piece is complete (all blocks received)
	if piece.receivedBlocks == piece.totalBlocks {
		if m.verifyPiece(index, piece.data) {
			m.completed[index] = true
			delete(m.pendingPieces, index)
			delete(m.inProgress, index)
			m.pieceResults <- PieceResult{
				Index: index,
				Data:  piece.data,
			}
		} else {
			m.handleCorruptPiece(index)
		}
	}
}

// verifyPiece checks the assembled piece data against the expected SHA1 hash.
func (m *Manager) verifyPiece(index uint32, data []byte) bool {
	hash := sha1.Sum(data)
	return bytes.Equal(hash[:], m.pieceHashes[index][:])
}

// handleCorruptPiece clears the corrupted piece state so it can be retried.
func (m *Manager) handleCorruptPiece(index uint32) {
	delete(m.pendingPieces, index)
	delete(m.inProgress, index)

	// Recreate the buffer for retry.
	pieceSize := m.pieceLength
	if int(index) == m.totalPieces-1 {
		pieceSize = m.totalLength - uint64(index)*m.pieceLength
	}
	m.pendingPieces[index] = newPieceBuffer(pieceSize)
}

// newPieceBuffer allocates a PieceBuffer for the given piece size.
func newPieceBuffer(pieceSize uint64) *PieceBuffer {
	totalBlocks := int((pieceSize + blockSize - 1) / blockSize)
	return &PieceBuffer{
		data:           make([]byte, pieceSize),
		totalBlocks:    totalBlocks,
		receivedBlocks: 0,
	}
}

func (m *Manager) isInProgress(index uint32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.inProgress[index]
	return ok
}

func (m *Manager) markInProgress(index uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inProgress[index] = true
}

func (m *Manager) markNotInProgress(index uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.inProgress, index)
}

func (m *Manager) markCompleted(index uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completed[index] = true
}

func (m *Manager) isCompleted(index uint32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.completed[index]
}