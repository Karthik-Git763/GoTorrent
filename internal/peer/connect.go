package peer

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"go-torrent/internal/torrent"
	"go-torrent/internal/tracker"
)

const peerIDPrefix = "-GT0001-"

// ConnectFailure records where an outbound peer connection failed.
type ConnectFailure struct {
	Address string
	Stage   string
	Err     error
}

// ConnectReport summarizes one batch of outbound peer connection attempts.
type ConnectReport struct {
	Attempted  int
	Dialed     int
	Handshaken int
	Failures   []ConnectFailure
}

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
	connections, _ := ConnectToPeersWithID(torrent, peers, GeneratePeerID())
	return connections
}

// ConnectToPeersWithID connects to peers using the same peer ID that was sent
// to the tracker and returns diagnostics for failed connection stages.
func ConnectToPeersWithID(torrent *torrent.TorrentFile, peers []tracker.Peer, localPeerID [20]byte) ([]*PeerConnection, ConnectReport) {
	var (
		mu          sync.Mutex
		connections []*PeerConnection
		wg          sync.WaitGroup
		report      = ConnectReport{Attempted: len(peers)}
	)
	recordFailure := func(address, stage string, err error) {
		mu.Lock()
		report.Failures = append(report.Failures, ConnectFailure{Address: address, Stage: stage, Err: err})
		mu.Unlock()
	}

	for _, p := range peers {
		wg.Go(func() {
			// TCP connect with timeout
			addr := net.JoinHostPort(p.IP.String(), fmt.Sprintf("%d", p.Port))
			conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
			if err != nil {
				recordFailure(addr, "dial", err)
				return
			}
			mu.Lock()
			report.Dialed++
			mu.Unlock()

			// BitTorrent handshake
			remoteID, err := Handshake(conn, torrent.InfoHash, localPeerID)
			if err != nil {
				conn.Close()
				recordFailure(addr, "handshake", err)
				return
			}
			mu.Lock()
			report.Handshaken++
			mu.Unlock()

			// Start goroutine pair (reader + writer)
			pc := NewPeerConnection(conn, remoteID)
			pc.Start()
			if !pc.SendInterested() {
				pc.Close()
				recordFailure(addr, "interested", fmt.Errorf("connection closed"))
				return
			}

			// Read an optional initial bitfield. BEP 3 peers with no pieces may skip it.
			pc.SetBitfield(make([]bool, len(torrent.PieceHashes)))
			select {
			case msg, ok := <-pc.Incoming():
				if !ok {
					pc.Close()
					recordFailure(addr, "initial message", fmt.Errorf("connection closed"))
					return
				}
				if msg.ID == MsgBitfield {
					pc.SetBitfield(ParseBitfield(msg.Payload, len(torrent.PieceHashes)))
				} else if msg.ID == MsgHave && len(msg.Payload) == 4 {
					idx := binary.BigEndian.Uint32(msg.Payload)
					bf := pc.Bitfield()
					if int(idx) < len(bf) {
						bf[idx] = true
					}
				}
			case <-time.After(10 * time.Second):
				// No bitfield is valid; continue and learn availability from Have.
			case <-pc.Done():
				recordFailure(addr, "initial message", fmt.Errorf("connection closed"))
				return
			}

			// Add to connections (thread-safe)
			mu.Lock()
			connections = append(connections, pc)
			mu.Unlock()
		})
	}

	wg.Wait()
	return connections, report
}
