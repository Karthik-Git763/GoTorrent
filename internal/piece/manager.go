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

// Manager orchestrates piece downloading across multiple peers.
type Manager struct {
	totalPieces  int
	pieceLength  uint64
	totalLength  uint64
	pieceHashes  [][20]byte
	peers        []*peer.PeerConnection
	completed    []bool
	pieceResults chan PieceResult // workers -> manager collector
	outputName   string          // torrent name for output files
	files        []torrent.FileEntry // multi-file entries (nil for single-file)

	mu         sync.Mutex
	inProgress map[uint32]bool

	// Resume support
	savePath string         // path to .gtstate file (empty = no periodic saves)
	infoHash [20]byte       // for matching saved state

	// Upload support
	pw *PieceWriter // set during Download for serving uploaded pieces
}

// NewManager creates a Manager ready to start downloading.
func NewManager(torrent *torrent.TorrentFile, peers []*peer.PeerConnection) *Manager {
	return &Manager{
		totalPieces:  len(torrent.PieceHashes),
		pieceLength:  uint64(torrent.PieceLength),
		totalLength:  uint64(torrent.Length),
		pieceHashes:  torrent.PieceHashes,
		peers:        peers,
		outputName:   torrent.Name,
		files:        torrent.Files,
		completed:    make([]bool, len(torrent.PieceHashes)),
		pieceResults: make(chan PieceResult, 32),
		inProgress:   make(map[uint32]bool),
	}
}

// Download runs the full download pipeline:
//  1. create the output file(s) and serve uploaded pieces from them
//  2. launch one worker goroutine per connected peer
//  3. each worker independently selects pieces via RarestPiece/FirstPiece
//  4. workers request blocks, assemble pieces, verify SHA1 hashes
//  5. collector writes verified pieces to the output file(s)
//
// When resume is true, the file writer preserves existing data and the
// output is pre-allocated so WriteAt works at any offset. Already-completed
// pieces (from m.completed) are skipped.
//
// If EnablePeriodicSave was called before Download, the manager saves
// progress periodically during the download.
//
// Blocks until all pieces are downloaded or an error occurs.
func (m *Manager) Download(outputPath string, resume bool) error {
	if len(m.peers) == 0 {
		return fmt.Errorf("no peers available")
	}

	// If all pieces already completed, nothing to do
	if m.allCompleted() {
		fmt.Printf("All %d pieces already completed\n", m.totalPieces)
		return nil
	}

	// Create the piece writer for the output file(s)
	tf := &torrent.TorrentFile{
		PieceLength: int64(m.pieceLength),
		Length:      int64(m.totalLength),
		Name:        m.outputName,
		Files:       m.files,
	}
	pw, err := NewPieceWriter(outputPath, tf, resume)
	if err != nil {
		return fmt.Errorf("creating piece writer: %w", err)
	}
	defer pw.Close()
	m.pw = pw

	// Set up upload handlers on all peers so they can request pieces from us.
	m.initUpload()

	// Workers: one per peer, each selecting and downloading pieces independently
	var wg sync.WaitGroup
	for _, p := range m.peers {
		wg.Go(func() {
			m.worker(p)
		})
	}

	// Close piece results when all workers exit
	go func() {
		wg.Wait()
		close(m.pieceResults)
	}()

	// Collector: read piece results, write to file, and track progress
	completedPieces := CountCompleted(m.completed)
	lastSaveCount := completedPieces

	for result := range m.pieceResults {
		if result.Err != nil {
			return result.Err
		}
		if result.Index >= uint32(len(m.completed)) || m.completed[result.Index] {
			continue
		}

		if err := pw.WritePiece(result.Index, result.Data); err != nil {
			return fmt.Errorf("writing piece %d: %w", result.Index, err)
		}

		m.completed[result.Index] = true
		completedPieces++

		// Announce the new piece to all connected peers so they can request it.
		m.announceHave(result.Index)

		fmt.Printf("Progress %d/%d pieces (%.1f%%)\n",
			completedPieces, m.totalPieces,
			float64(completedPieces)/float64(m.totalPieces)*100)

		// Periodic state save
		if m.savePath != "" && completedPieces-lastSaveCount >= saveInterval {
			if err := SaveResume(m.savePath, m.infoHash, m.completed); err != nil {
				fmt.Printf("Warning: failed to save resume state: %v\n", err)
			}
			lastSaveCount = completedPieces
		}
	}

	return nil
}

// initUpload sets up upload handling on all peer connections:
//  1. registers the getPieceData callback so the writer goroutine can
//     respond to remote peer requests by reading from disk
//  2. sends our bitfield to each peer so they know what pieces we have
func (m *Manager) initUpload() {
	bitfieldBytes := peer.BuildBitfieldBytes(m.completed)

	for _, p := range m.peers {
		p.SetPieceDataHandler(func(work peer.PieceWork) ([]byte, bool) {
			m.mu.Lock()
			completed := int(work.Index) < len(m.completed) && m.completed[work.Index]
			m.mu.Unlock()

			if !completed || m.pw == nil {
				return nil, false
			}

			pieceSize := m.pieceLength
			if int(work.Index) == m.totalPieces-1 {
				pieceSize = m.totalLength - uint64(work.Index)*m.pieceLength
			}

			// Clamp length to piece boundary
			length := work.Length
			if uint64(work.Begin)+uint64(length) > pieceSize {
				length = int(pieceSize - uint64(work.Begin))
			}
			if length <= 0 {
				return nil, false
			}

			data := make([]byte, length)
			n, err := m.pw.ReadPiece(uint32(work.Index), data)
			if err != nil || n != len(data) {
				return nil, false
			}
			return data, true
		})

		// Send our bitfield so the peer knows what we have.
		if len(bitfieldBytes) > 0 {
			p.SendBitfield(bitfieldBytes)
		}
	}
}

// announceHave sends a Have message to all connected peers when we complete a piece.
func (m *Manager) announceHave(index uint32) {
	for _, p := range m.peers {
		p.SendHave(index)
	}
}

// nextPiece selects the best eligible piece for the given peer using a
// two-tier strategy:
//  1. RarestPiece
//  2. FirstPiece
// Returns -1 if no eligible piece exists for this peer.
func (m *Manager) nextPiece(p *peer.PeerConnection) int {
	idx := m.RarestPiece(p)
	if idx != -1 {
		return idx
	}
	return m.FirstPiece(p)
}

// worker selects pieces for its peer via nextPiece, then downloads all
// blocks of each piece. It requests all blocks of a piece at once
// (pipelining), collects the responses, verifies the SHA1 hash, and
// sends the verified piece to pieceResults. On corruption it loops
// back to re-select the piece; on disconnection it exits so the peer's
// in-progress pieces become available to other workers.
func (m *Manager) worker(p *peer.PeerConnection) {
	for {
		idx := m.nextPiece(p)
		if idx == -1 {
			return // no more work for this peer
		}

		pieceSize := m.pieceLength
		if int(idx) == m.totalPieces-1 {
			pieceSize = m.totalLength - uint64(idx)*m.pieceLength
		}
		numBlocks := int((pieceSize + blockSize - 1) / blockSize)
		pieceData := make([]byte, pieceSize)

		m.markInProgress(uint32(idx))

		// Pipeline: send all block requests for this piece to the peer.
		ok := m.sendBlockRequests(p, idx, numBlocks, pieceSize)
		if !ok {
			m.markNotInProgress(uint32(idx))
			return // peer choked or disconnected
		}

		// Collect all block responses.
		ok = m.collectBlocks(p, idx, pieceData, numBlocks)
		if !ok {
			m.markNotInProgress(uint32(idx))
			return // peer disconnected mid-piece
		}

		// Verify SHA1 hash.
		if !m.verifyPiece(uint32(idx), pieceData) {
			m.markNotInProgress(uint32(idx))
			continue
		}

		m.pieceResults <- PieceResult{
			Index: uint32(idx),
			Data:  pieceData,
		}
	}
}

// sendBlockRequests sends all block requests for the given piece to the peer.
// Returns false if the peer can't accept any request (choked/disconnected).
func (m *Manager) sendBlockRequests(p *peer.PeerConnection, idx, numBlocks int, pieceSize uint64) bool {
	for b := range numBlocks {
		begin := int64(b) * blockSize
		length := int64(blockSize)
		if begin+length > int64(pieceSize) {
			length = int64(pieceSize) - begin
		}

		ok := p.AssignWork(peer.PieceWork{
			Index:  int(idx),
			Begin:  int(begin),
			Length: int(length),
		})
		if !ok {
			return false
		}
	}
	return true
}

// collectBlocks reads responses from the peer until all blocks of the piece
// are received. Returns false if the peer disconnects before completion.
// In endgame mode, another peer may complete the same piece while we're
// collecting — if m.completed[idx] becomes true, we exit early.
func (m *Manager) collectBlocks(p *peer.PeerConnection, idx int, pieceData []byte, numBlocks int) bool {
	for remaining := numBlocks; remaining > 0; {
		if m.isCompleted(uint32(idx)) {
			return false
		}

		msg := m.waitForPieceMessage(p)
		if msg == nil {
			return false
		}

		begin := binary.BigEndian.Uint32(msg.Payload[4:8])
		blockData := msg.Payload[8:]
		copy(pieceData[begin:], blockData)
		remaining--
	}
	return true
}

// waitForPieceMessage reads from the peer's Incoming channel until a MsgPiece
// is received, tracking Have messages along the way. Returns nil if the peer
// disconnects before delivering the piece.
func (m *Manager) waitForPieceMessage(p *peer.PeerConnection) *peer.Message {
	for {
		select {
		case msg, ok := <-p.Incoming():
			if !ok {
				return nil
			}
			if msg.ID == peer.MsgPiece {
				return msg
			}
			// Track have messages to keep bitfield current.
			if msg.ID == peer.MsgHave && len(msg.Payload) == 4 {
				idx := binary.BigEndian.Uint32(msg.Payload)
				bf := p.Bitfield()
				if int(idx) < len(bf) {
					bf[idx] = true
				}
			}
		case <-p.Done():
			return nil
		}
	}
}

// verifyPiece checks the assembled piece data against the expected SHA1 hash.
func (m *Manager) verifyPiece(index uint32, data []byte) bool {
	hash := sha1.Sum(data)
	return bytes.Equal(hash[:], m.pieceHashes[index][:])
}

// Helpers 

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
	return int(index) < len(m.completed) && m.completed[index]
}

// allCompleted returns true when every piece is marked done.
func (m *Manager) allCompleted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.completed {
		if !c {
			return false
		}
	}
	return true
}

// SetCompleted overrides the completed bitfield with a saved state.
func (m *Manager) SetCompleted(completed []bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(completed) == len(m.completed) {
		copy(m.completed, completed)
	}
}

// Completed returns a copy of the completed bitfield for external save.
func (m *Manager) Completed() []bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]bool, len(m.completed))
	copy(out, m.completed)
	return out
}

// EnablePeriodicSave sets the manager up to save progress to disk
// every saveInterval completed pieces during Download.
func (m *Manager) EnablePeriodicSave(savePath string, infoHash [20]byte) {
	m.savePath = savePath
	m.infoHash = infoHash
}