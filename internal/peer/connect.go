package peer

import (
	"crypto/rand"
	"fmt"
	"net"
	"sync"
	"time"

	"go-torrent/internal/torrent"
	"go-torrent/internal/tracker"
)

const peerIDPrefix = "-GT0001-"

// GeneratePeerID creates a 20-byte BitTorrent peer ID with GoTorrent's client prefix.
func GeneratePeerID() [20]byte {
	var id [20]byte
	copy(id[:], peerIDPrefix) // 8 bytes: "-GT0001-"

	// Fill remaining 12 bytes with random printable chars
	var buf [12]byte
	_, _ = rand.Read(buf[:])
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	for i, b := range buf {
		id[8+i] = chars[int(b)%len(chars)]
	}
	return id
}

// countSetBits returns the number of true values in a bool slice.
func countSetBits(bits []bool) int {
	n := 0
	for _, b := range bits {
		if b {
			n++
		}
	}
	return n
}

// ConnectToPeers opens TCP connections to each peer, performs the BitTorrent
// handshake, reads the initial bitfield, and sends Interested.
//
// Returns only the peers that successfully completed the full handshake flow.
// Unreachable or slow peers are skipped silently.
func ConnectToPeers(torrent *torrent.TorrentFile, peers []tracker.Peer) []*PeerConnection {
	var (
		mu          sync.Mutex
		connections []*PeerConnection
		wg          sync.WaitGroup
	)

	for _, p := range peers {
		wg.Go(func() {
			// TCP connect with timeout
			addr := net.JoinHostPort(p.IP.String(), fmt.Sprintf("%d", p.Port))
			conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
			if err != nil {
				return // peer unreachable, skip silently
			}

			// BitTorrent handshake
			peerID := GeneratePeerID()
			remoteID, err := Handshake(conn, torrent.InfoHash, peerID)
			if err != nil {
				conn.Close()
				return
			}

			// Start goroutine pair (reader + writer)
			pc := NewPeerConnection(conn, remoteID)
			pc.Start()

			// Read initial bitfield (first message after handshake)
			select {
			case msg := <-pc.Incoming():
				if msg.ID == MsgBitfield {
					pc.SetBitfield(ParseBitfield(msg.Payload, len(torrent.PieceHashes)))
				}
			case <-time.After(10 * time.Second):
				pc.Close()
				return // no bitfield within 10s, skip peer
			}

			// Send Interested
			if err := WriteMessage(conn, &Message{ID: MsgInterested}); err != nil {
				pc.Close()
				return
			}

			// Add to connections (thread-safe)
			mu.Lock()
			connections = append(connections, pc)
			mu.Unlock()
		})
	}

	wg.Wait()
	return connections
}