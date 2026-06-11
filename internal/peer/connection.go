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

type PeerConnection struct {
	conn net.Conn
	peerID [20]byte
	choked atomic.Bool
	bitfield []bool
	incoming   chan *Message   // reader -> manager
	pieceQueue chan PieceWork  // manager -> writer
	ctx context.Context
	cancel context.CancelFunc
	closeOnce sync.Once // ensures Close is idempotent

	// Choked wait: writer parks here when choked, reader signals on unchoke
	unchokeMu   sync.Mutex
	unchokeCond *sync.Cond
}

type PieceWork struct {
	Index int
	Begin int
	Length int
}

func NewPeerConnection(conn net.Conn, peerID [20]byte) *PeerConnection {
	ctx, cancel := context.WithCancel(context.Background())
	pc := &PeerConnection{
		conn: conn,
		peerID: peerID,
		incoming: make(chan *Message, 100),
		pieceQueue: make(chan PieceWork, 10),
		ctx: ctx,
		cancel: cancel,
	}
	pc.choked.Store(true)        // start choked
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

		// Update internal state BEFORE delivering to incoming,
		// so the writer sees the correct choked state immediately.
		switch msg.ID {
		case MsgChoke:
			pc.choked.Store(true)
		case MsgUnchoke:
			pc.choked.Store(false)
			// Wake the writer so it can resume sending requests.
			pc.unchokeCond.Broadcast()
		}

		select {
		case pc.incoming <- msg:
		case <-pc.ctx.Done():
			return
		}
	}
}

func (pc *PeerConnection) AssignWork(work PieceWork) bool{
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

			if err := pc.conn.SetWriteDeadline(time.Now().Add(defaultTimeout)); err != nil {
				return // connection lost
			}
			if err := WriteMessage(pc.conn, msg); err != nil {
				return // connection lost
			}
		case <-pc.ctx.Done():
			return
		}
	}
}

// waitForUnchoke blocks until the peer unchokes us or the context is cancelled.
// Uses a sync.Cond so the writer parks without burning CPU.
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

	return &Message{
		ID: MsgRequest,
		Payload: payload,
	}
}

func (pc *PeerConnection) Close() {
	pc.closeOnce.Do(func() { // if multiple Close calls are made, only the first one will execute else close(pc.pieceQueue) causes a panic
		pc.cancel()
		pc.choked.Store(false) // wake the writer out of waitForUnchoke
		pc.unchokeCond.Broadcast()
		close(pc.pieceQueue)
		close(pc.incoming)
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
