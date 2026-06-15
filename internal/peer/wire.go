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

	return &Message{
		ID:      MessageID(payloadBuf[0]),
		Payload: payloadBuf[1:],
	}, nil
}

func WriteMessage(w io.Writer, msg *Message) error {
	// length = 1 (ID) + len(Payload)
	buf := make([]byte, 4+1+len(msg.Payload)) // 4 bytes for length, 1 byte for ID, len(Payload) bytes for payload
	binary.BigEndian.PutUint32(buf[0:4], uint32(1+len(msg.Payload))) // store the length of the payload (1 byte for ID + len(Payload))
	buf[4] = byte(msg.ID)
	copy(buf[5:], msg.Payload)
	_, err := w.Write(buf)
	return err
}

func ParseBitfield(payload []byte, totalPieces int) []bool {
	bitfield := make([]bool, totalPieces)
	for i := range totalPieces {
		byteIndex := i / 8
		bitIndex := 7 - (i % 8) // MSB first
		if byteIndex < len(payload) {
			bitfield[i] = (payload[byteIndex]>>bitIndex)&1 == 1
		}
	}
	return bitfield
}

// BuildBitfieldBytes encodes a bool slice into the wire-format byte slice.
func BuildBitfieldBytes(bits []bool) []byte {
	if len(bits) == 0 {
		return nil
	}
	size := (len(bits) + 7) / 8
	buf := make([]byte, size)
	for i, set := range bits {
		if !set {
			continue
		}
		byteIndex := i / 8
		bitIndex := 7 - (i % 8)
		buf[byteIndex] |= 1 << bitIndex
	}
	return buf
}

// BuildPiece constructs a MsgPiece response for the given index, begin offset, and block data.
func BuildPiece(index, begin uint32, data []byte) *Message {
	payload := make([]byte, 8+len(data))
	binary.BigEndian.PutUint32(payload[0:4], index)
	binary.BigEndian.PutUint32(payload[4:8], begin)
	copy(payload[8:], data)
	return &Message{ID: MsgPiece, Payload: payload}
}

// BuildHave constructs a MsgHave for the given piece index.
func BuildHave(index uint32) *Message {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, index)
	return &Message{ID: MsgHave, Payload: payload}
}