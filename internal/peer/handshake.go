package peer

import (
	"errors"
	"io"
	"net"
	"time"
)

var ErrInvalidHandshake = errors.New("Invalid Handshake")

// Handshake sends a handshake message to the peer and verifies the response.
func Handshake(conn net.Conn, infoHash [20]byte, peerID [20]byte) (remotePeerID [20]byte, err error) {
	// Build handshake
	buf := make([]byte, 68)

	// Protocol identifier
	buf[0] = 19
	copy(buf[1:], []byte("BitTorrent protocol"))
	// 20 - 27 are reserved
	copy(buf[28:], infoHash[:])
	copy(buf[48:], peerID[:])

	// Set deadline
	err = conn.SetDeadline(time.Now().Add(10*time.Second))
	if err != nil {
		return
	}
	defer conn.SetDeadline(time.Time{})

	// Send handshake
	_, err = conn.Write(buf)
	if err != nil {
		return
	}
	
	// Read response
	resp := make([]byte, 68)
	// Since TCP is a stream it may give 1 byte when conn.Read() is called, so we use io.ReadFull to ensure we get 68 bytes
	_, err = io.ReadFull(conn, resp)
	if err != nil {
		return
	}

	// Verify protocol
	const protocol = "BitTorrent protocol"
	if resp[0] != byte(len(protocol)) {
		err = ErrInvalidHandshake
		return
	}
	if string(resp[1:20]) != protocol {
		err = ErrInvalidHandshake
		return
	}
	// Verify infoHash
	var infoHashResp [20]byte
	copy(infoHashResp[:], resp[28:48])
	if infoHash != infoHashResp {
		err = ErrInvalidHandshake
		return
	}
	// Verify peerID
	copy(remotePeerID[:], resp[48:68])
	return
}

