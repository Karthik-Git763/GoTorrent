package peer

import (
	"bytes"
	"testing"
)

func TestReadMessage_Keepalive(t *testing.T) {
	// Length=0 → returns nil, nil (keep-alive)
	buf := []byte{0, 0, 0, 0}
	msg, err := ReadMessage(bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != nil {
		t.Fatalf("expected nil for keep-alive, got %+v", msg)
	}
}

func TestReadMessage_Choke(t *testing.T) {
	buf := []byte{0, 0, 0, 1, 0}
	msg, err := ReadMessage(bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ID != MsgChoke {
		t.Fatalf("expected MsgChoke (0), got %d", msg.ID)
	}
	if len(msg.Payload) != 0 {
		t.Fatalf("expected empty payload, got %d bytes", len(msg.Payload))
	}
}

func TestReadMessage_Unchoke(t *testing.T) {
	buf := []byte{0, 0, 0, 1, 1}
	msg, err := ReadMessage(bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ID != MsgUnchoke {
		t.Fatalf("expected MsgUnchoke (1), got %d", msg.ID)
	}
}

func TestReadMessage_Interested(t *testing.T) {
	buf := []byte{0, 0, 0, 1, 2}
	msg, err := ReadMessage(bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ID != MsgInterested {
		t.Fatalf("expected MsgInterested (2), got %d", msg.ID)
	}
}

func TestReadMessage_Have(t *testing.T) {
	buf := []byte{0, 0, 0, 5, 4, 0, 0, 0, 42}
	msg, err := ReadMessage(bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ID != MsgHave {
		t.Fatalf("expected MsgHave (4), got %d", msg.ID)
	}
	if len(msg.Payload) != 4 {
		t.Fatalf("expected 4 byte payload, got %d", len(msg.Payload))
	}
	if msg.Payload[3] != 42 {
		t.Fatalf("expected piece index 42, got %d", msg.Payload[3])
	}
}

func TestReadMessage_Bitfield(t *testing.T) {
	buf := []byte{0, 0, 0, 2, 5, 0b10110001}
	msg, err := ReadMessage(bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ID != MsgBitfield {
		t.Fatalf("expected MsgBitfield (5), got %d", msg.ID)
	}
	if len(msg.Payload) != 1 {
		t.Fatalf("expected 1 byte payload, got %d", len(msg.Payload))
	}
	if msg.Payload[0] != 0b10110001 {
		t.Fatalf("expected bitmask 0b10110001, got 0b%08b", msg.Payload[0])
	}
}

func TestReadMessage_Request(t *testing.T) {
	buf := []byte{0, 0, 0, 13, 6, 0, 0, 0, 5, 0, 0, 64, 0, 0, 0, 64, 0}
	msg, err := ReadMessage(bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ID != MsgRequest {
		t.Fatalf("expected MsgRequest (6), got %d", msg.ID)
	}
	if len(msg.Payload) != 12 {
		t.Fatalf("expected 12 byte payload, got %d", len(msg.Payload))
	}
}

func TestReadMessage_Piece(t *testing.T) {
	// Length=10, ID=7, payload=index(4)+begin(4)+data(1)
	data := []byte{0xAB}
	buf := append([]byte{0, 0, 0, 10, 7, 0, 0, 0, 5, 0, 0, 0, 0}, data...)
	msg, err := ReadMessage(bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ID != MsgPiece {
		t.Fatalf("expected MsgPiece (7), got %d", msg.ID)
	}
	if len(msg.Payload) != 9 {
		t.Fatalf("expected 9 byte payload, got %d", len(msg.Payload))
	}
}

func TestWriteMessage_Choke(t *testing.T) {
	var buf bytes.Buffer
	err := WriteMessage(&buf, &Message{ID: MsgChoke})
	if err != nil {
		t.Fatalf("WriteMessage error: %v", err)
	}
	// Length=1, ID=0
	want := []byte{0, 0, 0, 1, 0}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("got %v, want %v", buf.Bytes(), want)
	}
}

func TestWriteMessage_Have(t *testing.T) {
	var buf bytes.Buffer
	err := WriteMessage(&buf, &Message{ID: MsgHave, Payload: []byte{0, 0, 0, 99}})
	if err != nil {
		t.Fatalf("WriteMessage error: %v", err)
	}
	want := []byte{0, 0, 0, 5, 4, 0, 0, 0, 99}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("got %v, want %v", buf.Bytes(), want)
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  *Message
	}{
		{"choke", &Message{ID: MsgChoke}},
		{"unchoke", &Message{ID: MsgUnchoke}},
		{"interested", &Message{ID: MsgInterested}},
		{"not-interested", &Message{ID: MsgNotInterested}},
		{"have", &Message{ID: MsgHave, Payload: []byte{0, 0, 0, 7}}},
		{"bitfield", &Message{ID: MsgBitfield, Payload: []byte{0xFF, 0x00}}},
		{"request", &Message{ID: MsgRequest, Payload: []byte{0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 64, 0}}},
		{"piece", &Message{ID: MsgPiece, Payload: []byte{0, 0, 0, 5, 0, 0, 0, 0, 1, 2, 3, 4}}},
		{"cancel", &Message{ID: MsgCancel, Payload: []byte{0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 64, 0}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteMessage(&buf, tt.msg); err != nil {
				t.Fatalf("WriteMessage error: %v", err)
			}
			got, err := ReadMessage(&buf)
			if err != nil {
				t.Fatalf("ReadMessage error: %v", err)
			}
			if got.ID != tt.msg.ID {
				t.Fatalf("ID: got %d, want %d", got.ID, tt.msg.ID)
			}
			if !bytes.Equal(got.Payload, tt.msg.Payload) {
				t.Fatalf("Payload: got %x, want %x", got.Payload, tt.msg.Payload)
			}
		})
	}
}

func TestParseBitfield(t *testing.T) {
	payload := []byte{0b10110001}
	got := ParseBitfield(payload, 8)
	want := []bool{true, false, true, true, false, false, false, true}
	if len(got) != len(want) {
		t.Fatalf("length: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bit %d: got %v, want %v (MSB first)", i, got[i], want[i])
		}
	}
}

func TestParseBitfield_PartialByte(t *testing.T) {
	// 5 pieces in 1 byte — 3 trailing bits should be false
	payload := []byte{0b10111000} // top 5 bits set
	got := ParseBitfield(payload, 5)
	want := []bool{true, false, true, true, true}
	if len(got) != len(want) {
		t.Fatalf("length: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bit %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

func TestParseBitfield_MultiByte(t *testing.T) {
	payload := []byte{0xFF, 0x00, 0b10101010, 0b11110000}
	got := ParseBitfield(payload, 32)
	// Byte 0: all true
	for i := range 8 {
		if !got[i] {
			t.Fatalf("bit %d: expected true", i)
		}
	}
	// Byte 1: all false
	for i := 8; i < 16; i++ {
		if got[i] {
			t.Fatalf("bit %d: expected false", i)
		}
	}
	// Byte 2: 10101010
	if got[16] != true || got[17] != false || got[18] != true || got[19] != false {
		t.Fatalf("byte 2 bits wrong: got %v", got[16:24])
	}
	// Byte 3: 11110000
	if got[24] != true || got[25] != true || got[26] != true || got[27] != true || got[28] != false {
		t.Fatalf("byte 3 bits wrong: got %v", got[24:32])
	}
}

func TestBuildRequest(t *testing.T) {
	msg := BuildRequest(5, 16384, 16384)
	if msg.ID != MsgRequest {
		t.Fatalf("expected MsgRequest, got %d", msg.ID)
	}
	if len(msg.Payload) != 12 {
		t.Fatalf("expected 12 byte payload, got %d", len(msg.Payload))
	}
	// index=5, begin=16384(0x4000), length=16384(0x4000)
	want := []byte{0, 0, 0, 5, 0, 0, 64, 0, 0, 0, 64, 0}
	if !bytes.Equal(msg.Payload, want) {
		t.Fatalf("payload: got %x, want %x", msg.Payload, want)
	}
}