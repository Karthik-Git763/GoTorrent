package piece

import (
	"math/rand"

	"go-torrent/internal/peer"
)

// RarestPiece selects the piece with the lowest availability among this peer's
// pieces, skipping completed and in-progress pieces. Ties are broken randomly.
// Returns -1 if no eligible piece exists.
func (m *Manager) RarestPiece(p *peer.PeerConnection) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Count availability across all connected peers.
	availability := make([]int, m.totalPieces)
	for _, peer := range m.peers {
		bf := peer.Bitfield()
		for i := range m.totalPieces {
			if i < len(bf) && bf[i] {
				availability[i]++
			}
		}
	}

	// First pass: find the minimum availability among eligible pieces.
	rarestCount := len(m.peers) + 1 // larger than any possible count
	for i := range m.totalPieces {
		if m.completed[i] || m.inProgress[uint32(i)] {
			continue
		}
		bf := p.Bitfield()
		if i >= len(bf) || !bf[i] {
			continue
		}
		if availability[i] < rarestCount {
			rarestCount = availability[i]
		}
	}

	if rarestCount > len(m.peers) {
		return -1 // no eligible piece found
	}

	// Second pass: collect all eligible pieces at that rarest availability.
	var candidates []int
	for i := range m.totalPieces {
		if m.completed[i] || m.inProgress[uint32(i)] {
			continue
		}
		bf := p.Bitfield()
		if i >= len(bf) || !bf[i] {
			continue
		}
		if availability[i] == rarestCount {
			candidates = append(candidates, i)
		}
	}

	if len(candidates) == 0 {
		return -1
	}
	return candidates[rand.Intn(len(candidates))]
}

// FirstPiece returns the first available piece for the given peer that hasn't
// been completed. Useful as a fallback or for the initial piece selection.
// Returns -1 if no available piece exists.
func (m *Manager) FirstPiece(p *peer.PeerConnection) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	bf := p.Bitfield()
	for i := range m.totalPieces {
		if i < len(bf) && bf[i] && !m.completed[i] {
			return i
		}
	}
	return -1
}