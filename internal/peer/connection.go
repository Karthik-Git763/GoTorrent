package peer

import (
	"context"
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type PeerConnection struct {
	conn net.Conn
	peerID [20]byte
	choked atomic.Bool
	bitfield []bool
	incoming chan *Message // reader -> manager
	pieceQueue chan PieceWork // manager -> writer
	ctx context.Context
	cancel context.CancelFunc
	closeOnce sync.Once // ensures Close is idempotent
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
	pc.choked.Store(true) // start choked
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
		if err := pc.conn.SetReadDeadline(time.Now().Add(30*time.Second)); err != nil {
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

		select {
		case pc.incoming <- msg:
			switch msg.ID {
			case MsgChoke:
				pc.choked.Store(true)
			case MsgUnchoke:
				pc.choked.Store(false)
			}
		case <-pc.ctx.Done():
			return
		}
	}
}

func (pc *PeerConnection) AssignWork(work PieceWork) {
	select {
	case pc.pieceQueue <- work:
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

			if pc.choked.Load() {
				continue
			}
			
			msg := BuildRequest(work.Index, work.Begin, work.Length)

			if err := pc.conn.SetWriteDeadline(time.Now().Add(30*time.Second)); err != nil {
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
		close(pc.pieceQueue)
		close(pc.incoming)
		pc.conn.Close()
	})
}
