package peer

import (
	"context"
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const defaultTimeout = 30 * time.Second

// PeerConnection manages a single peer's TCP connection with a reader/writer
// goroutine pair. Supports both downloading (requesting blocks) and uploading
// (responding to peer requests).
type PeerConnection struct {
	conn   net.Conn
	peerID [20]byte
	choked atomic.Bool
	bitfield []bool
	incoming   chan *Message  // reader -> manager (wire messages except requests)
	pieceQueue chan PieceWork // manager -> writer (download requests to send)
	pieceRequests chan PieceWork // reader -> writer (upload requests from remote peer)
	outgoing     chan *Message  // manager/reader -> writer (control messages: unchoke, have, bitfield)
	ctx context.Context
	cancel context.CancelFunc
	closeOnce sync.Once

	// Choked wait: writer parks here when choked, reader signals on unchoke
	unchokeMu   sync.Mutex
	unchokeCond *sync.Cond

	// Upload callback: set by Manager to serve piece data from disk
	getPieceData func(PieceWork) ([]byte, bool)
}

type PieceWork struct {
	Index  int
	Begin  int
	Length int
}

func NewPeerConnection(conn net.Conn, peerID [20]byte) *PeerConnection {
	ctx, cancel := context.WithCancel(context.Background())
	pc := &PeerConnection{
		conn:           conn,
		peerID:         peerID,
		incoming:       make(chan *Message, 100),
		pieceQueue:     make(chan PieceWork, 10),
		pieceRequests:  make(chan PieceWork, 10),
		outgoing:       make(chan *Message, 10),
		ctx:            ctx,
		cancel:         cancel,
	}
	pc.choked.Store(true)
	pc.unchokeCond = sync.NewCond(&pc.unchokeMu)
	return pc
}

func (pc *PeerConnection) Start() {
	go pc.reader()
	go pc.writer()
}

func (pc *PeerConnection) reader() {
	defer pc.Close()
	for {
		// Set deadline
		if err := pc.conn.SetReadDeadline(time.Now().Add(defaultTimeout)); err != nil {
			return
		}
		
		// Read data
		msg, err := ReadMessage(pc.conn)
		if err != nil {
			return // connection lost
		}
		if msg == nil {
			continue // keep-alive
		}

		// Handle messages that need routing before the generic incoming channel.
		switch msg.ID {
		case MsgChoke:
			pc.choked.Store(true)
		case MsgUnchoke:
			pc.choked.Store(false)
			// Wake the writer so it can resume sending requests.
			pc.unchokeCond.Broadcast()
		case MsgRequest:
			// Remote peer wants a piece from us — route to upload handler.
			if len(msg.Payload) >= 12 {
				index := int(binary.BigEndian.Uint32(msg.Payload[0:4]))
				begin := int(binary.BigEndian.Uint32(msg.Payload[4:8]))
				length := int(binary.BigEndian.Uint32(msg.Payload[8:12]))
				select {
				case pc.pieceRequests <- PieceWork{Index: index, Begin: begin, Length: length}:
				case <-pc.ctx.Done():
					return
				}
			}
			continue // don't send to incoming
		case MsgInterested:
			// Peer wants our pieces — auto-unchoke them immediately.
			pc.sendUnchoke()
			continue
		case MsgNotInterested:
			continue
		}

		// Route all other messages (piece, have, bitfield, etc.) to incoming.
		select {
		case pc.incoming <- msg:
		case <-pc.ctx.Done():
			return
		}
	}
}

// sendUnchoke sends an unchoke message to the remote peer via the writer.
func (pc *PeerConnection) sendUnchoke() {
	select {
	case pc.outgoing <- &Message{ID: MsgUnchoke}:
	case <-pc.ctx.Done():
	}
}

// AssignWork queues a download request for the writer to send to the peer.
// Returns false if the peer is choked or the connection is closing.
func (pc *PeerConnection) AssignWork(work PieceWork) bool {
	if pc.choked.Load() {
		return false
	}
	select {
	case pc.pieceQueue <- work:
		return true
	case <-pc.ctx.Done():
		return false
	}
}

// SetPieceDataHandler registers a callback that the writer uses to look up
// piece data when serving upload requests from the remote peer.
func (pc *PeerConnection) SetPieceDataHandler(handler func(PieceWork) ([]byte, bool)) {
	pc.getPieceData = handler
}

// SendHave queues a Have message to announce a newly completed piece.
func (pc *PeerConnection) SendHave(index uint32) {
	select {
	case pc.outgoing <- BuildHave(index):
	case <-pc.ctx.Done():
	}
}

// SendBitfield queues a Bitfield message to announce our available pieces.
func (pc *PeerConnection) SendBitfield(bits []byte) {
	select {
	case pc.outgoing <- &Message{ID: MsgBitfield, Payload: bits}:
	case <-pc.ctx.Done():
	}
}

func (pc *PeerConnection) writer() {
	defer pc.Close()
	for {
		select {
		case work, ok := <-pc.pieceQueue:
			if !ok {
				return
			}

			pc.waitForUnchoke()

			// If the context was cancelled while we were waiting, bail out.
			if pc.ctx.Err() != nil {
				return
			}

			msg := BuildRequest(work.Index, work.Begin, work.Length)
			if err := pc.writeMessage(msg); err != nil {
				return
			}

		case req, ok := <-pc.pieceRequests:
			if !ok {
				return
			}
			// Upload: remote peer requested a piece from us.
			if pc.getPieceData == nil {
				continue
			}
			data, ok := pc.getPieceData(req)
			if !ok || len(data) == 0 {
				continue
			}
			msg := BuildPiece(uint32(req.Index), uint32(req.Begin), data)
			if err := pc.writeMessage(msg); err != nil {
				return
			}

		case msg, ok := <-pc.outgoing:
			if !ok {
				return
			}
			if err := pc.writeMessage(msg); err != nil {
				return
			}

		case <-pc.ctx.Done():
			return
		}
	}
}

// writeMessage is a helper that sets the write deadline and writes to TCP.
func (pc *PeerConnection) writeMessage(msg *Message) error {
	if err := pc.conn.SetWriteDeadline(time.Now().Add(defaultTimeout)); err != nil {
		return err
	}
	return WriteMessage(pc.conn, msg)
}

// waitForUnchoke blocks until the peer unchokes us or the context is cancelled.
func (pc *PeerConnection) waitForUnchoke() {
	pc.unchokeMu.Lock()
	defer pc.unchokeMu.Unlock()
	for pc.choked.Load() {
		// Check if context was cancelled while we held the lock.
		if pc.ctx.Err() != nil {
			return
		}
		pc.unchokeCond.Wait()
	}
}

func BuildRequest(index, begin, length int) *Message {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], uint32(index))
	binary.BigEndian.PutUint32(payload[4:8], uint32(begin))
	binary.BigEndian.PutUint32(payload[8:12], uint32(length))
	return &Message{ID: MsgRequest, Payload: payload}
}

func (pc *PeerConnection) Close() {
	pc.closeOnce.Do(func() {
		pc.cancel()
		pc.choked.Store(false)
		pc.unchokeCond.Broadcast()
		pc.conn.Close()
	})
}

// SetBitfield updates the peer's bitfield (which pieces the peer has).
func (pc *PeerConnection) SetBitfield(bitfield []bool) {
	pc.bitfield = bitfield
}

// Bitfield returns the peer's current bitfield.
func (pc *PeerConnection) Bitfield() []bool {
	return pc.bitfield
}

// Incoming returns the read-only channel of messages from this peer.
func (pc *PeerConnection) Incoming() <-chan *Message {
	return pc.incoming
}

// Done returns a channel that's closed when the peer connection is closed.
// Workers can select on this alongside Incoming() to detect disconnection.
func (pc *PeerConnection) Done() <-chan struct{} {
	return pc.ctx.Done()
}