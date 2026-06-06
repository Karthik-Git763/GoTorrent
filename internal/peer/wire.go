package peer

import (
	"encoding/binary"
	"errors"
	"io"
)

type MessageID uint8

const (
	MsgChoke         MessageID = 0
	MsgUnchoke       MessageID = 1
	MsgInterested    MessageID = 2
	MsgNotInterested MessageID = 3
	MsgHave          MessageID = 4
	MsgBitfield      MessageID = 5
	MsgRequest       MessageID = 6
	MsgPiece         MessageID = 7
	MsgCancel        MessageID = 8
)

var ErrInvalidMessage = errors.New("invalid message")

type Message struct {
	ID      MessageID
	Payload []byte
}

func ReadMessage(r io.Reader) (*Message, error) {
	// Read the message length
	lenBuf := make([]byte, 4)
	_, err := io.ReadFull(r, lenBuf)
	if err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lenBuf)

	// Keep alive (length == 0)
	if length == 0 {
		return nil, nil
	}

	// Read Payload: ID (1 byte) + Payload (length - 1 bytes)
	payloadBuf := make([]byte, length)
	_, err = io.ReadFull(r, payloadBuf)
	if err != nil {
		return nil, err
	}

	// Parse
	msg := &Message{
		ID:      MessageID(payloadBuf[0]),
		Payload: payloadBuf[1:],
	}
	return msg, nil
}

func WriteMessage(w io.Writer, msg *Message) error {
	// length = 1 (ID) + len(Payload)
	buf := make([]byte, 4+1+len(msg.Payload))
	binary.BigEndian.PutUint32(buf[0:4], uint32(1+len(msg.Payload)))
	buf[4] = byte(msg.ID)
	copy(buf[5:], msg.Payload)
	_, err := w.Write(buf)
	return err
}

func ParseBitfield(payload []byte, totalPieces int) []bool {
	bitfield := make([]bool, totalPieces)
	for i := range totalPieces {
		byteIndex := i / 8
		bitIndex := 7 - (i % 8) // MSB  first!!
		if byteIndex < len(payload) {
			bitfield[i] = (payload[byteIndex]>>bitIndex)&1 == 1
		} else {
			bitfield[i] = false
		}
	}
	return bitfield
}
