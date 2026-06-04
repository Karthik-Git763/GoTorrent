package tracker

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"time"
)

type UDPTracker struct {
	conn *net.UDPConn
	transactionID uint32
	connectionID uint64
	connectionCreatedAt time.Time
	addr *net.UDPAddr
	started bool
}

// Actions
const (
	ActionConnect = 0
	ActionAnnounce = 1
	ActionScrape = 2
	ActionError = 3
)

// Events for announce 
const (
	EventNone = 0
	EventCompleted = 1
	EventStarted = 2
	EventStopped = 3
)

// Magic connection ID for UDP tracker
const MagicConnectionID uint64 = 0x41727101980

type ConnectionRequest struct {
	connectionID uint64
	action uint32
	transactionID uint32
}

type ConnectionResponse struct {
	action uint32
	transactionID uint32
	connectionID uint64
}

type AnnounceRequest struct {
	connectionID uint64
	action uint32
	transactionID uint32
	infoHash [20]byte
	peerID [20]byte
	downloaded int64
	uploaded int64
	left int64
	event int32
	IP net.IP
	port uint16
	numWant int32
	key int32
}

type AnnounceResponse struct {
	action uint32
	transactionID uint32
	interval uint32
	leechers uint32
	seeders uint32
	peers []Peer
}

func (t *UDPTracker) Connect(addr *net.UDPAddr) (uint64, error) {
	// Generate a random transaction ID
	t.transactionID = rand.Uint32()

	// Pack connection request: connID(8) + action(4) + transactionID(4)
	buf := make([]byte, 16)
	binary.BigEndian.PutUint64(buf[0:8], MagicConnectionID)
	binary.BigEndian.PutUint32(buf[8:12], uint32(ActionConnect))
	binary.BigEndian.PutUint32(buf[12:16], uint32(t.transactionID))

	// Send via net.DialUDP
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return 0, err
	}
	success := false
	defer func() {
		if !success {
			conn.Close()
		}
	}()
	
	// Read response (16 bytes) with timeout
	conn.SetReadDeadline(
		time.Now().Add(5*time.Second),
	)
	_, err = conn.Write(buf)
	if err != nil {
		return 0, err
	}

	// Read response
	buf = make([]byte, 16)
	n, err := conn.Read(buf)
	if err != nil {
		return 0, err
	}
	if n != 16 {
		return 0, fmt.Errorf("expected 16 bytes, got %d", n)
	}
	
	action := binary.BigEndian.Uint32(buf[0:4])
	if action == ActionError {
		return 0, fmt.Errorf("Tracker error %s", string(buf[8:n]))
	}
	
	resp := ConnectionResponse{
		action:         action,
		transactionID:  binary.BigEndian.Uint32(buf[4:8]),
		connectionID:   binary.BigEndian.Uint64(buf[8:16]),
	}
	
	// validate action == 0, transactionID matches
	if resp.action != ActionConnect {
		return 0, fmt.Errorf("expected connect action, got %d", resp.action)
	}
	if resp.transactionID != t.transactionID {
		return 0, fmt.Errorf("transaction ID mismatch")
	}
	success = true
	t.conn = conn
	t.connectionID = resp.connectionID
	t.connectionCreatedAt = time.Now()
	t.addr = addr
	// Return connection ID
	return resp.connectionID, nil
}

func (t *UDPTracker) Announce(infoHash [20]byte, peerID [20]byte, port uint16, totalLength uint64) ([]Peer, int, error) {
	if t.addr == nil {
		return nil, 0, fmt.Errorf("tracker address not set")
	}
	if err := t.EnsureConnection(); err != nil {
		return nil, 0, err
	}

	event := uint32(EventNone)
	if !t.started {
		event = EventStarted
		t.started = true
	}
	
	// Pack announce request: connID(8) + action(4) + transactionID(4) + infoHash(20) + peer_id(20)
	//  + download(8) + upload(8) + left(8) + event(4) + ip_addr(4) + key(4) + num_want(4) + port(2) - 98 bytes overhead
	buf := make([]byte, 98)
	binary.BigEndian.PutUint64(buf[0:8], t.connectionID)
	binary.BigEndian.PutUint32(buf[8:12], uint32(ActionAnnounce))
	announceTxID := rand.Uint32()
	binary.BigEndian.PutUint32(buf[12:16], announceTxID)
	copy(buf[16:36], infoHash[:])
	copy(buf[36:56], peerID[:])
	binary.BigEndian.PutUint64(buf[56:64], 0) // download
	binary.BigEndian.PutUint64(buf[64:72], totalLength) // left
	binary.BigEndian.PutUint64(buf[72:80], 0) // upload
	binary.BigEndian.PutUint32(buf[80:84], event) // event
	binary.BigEndian.PutUint32(buf[84:88], 0) // ip_addr
	binary.BigEndian.PutUint32(buf[88:92], rand.Uint32()) // key
	binary.BigEndian.PutUint32(buf[92:96], 0xFFFFFFFF) // num_want
	binary.BigEndian.PutUint16(buf[96:98], port)

	// Send announce request
	_, err := t.conn.Write(buf)
	if err != nil {
		return nil, 0, err
	}

	// read response with timeout
	timeout := time.Second * 5
	t.conn.SetReadDeadline(time.Now().Add(timeout))

	// response: action(4) + transactionID(4) + interval(4) + leechers(4) + seeders(4) + peers(...)
	buf = make([]byte, 1500)
	n, err := t.conn.Read(buf)
	if err != nil {
		return nil, 0, err
	}

	if n < 20 {
    	return nil, 0, fmt.Errorf("announce response too short")
	}
	action := binary.BigEndian.Uint32(buf[0:4])
	
	if action == ActionError {
		return nil, 0, fmt.Errorf("tracker error %s", string(buf[8:n]))
	}
	// Parse response
	resp := AnnounceResponse {
		action:        action,
		transactionID: binary.BigEndian.Uint32(buf[4:8]),
		interval:      binary.BigEndian.Uint32(buf[8:12]),
		leechers:      binary.BigEndian.Uint32(buf[12:16]),
		seeders:       binary.BigEndian.Uint32(buf[16:20]),
	}
	peerBytes := buf[20:n]

	if (n-20)%6 != 0 {
    	return nil, 0, fmt.Errorf("invalid peer data length")
	}
	peerCount := len(peerBytes) / 6

	resp.peers = make([]Peer, peerCount)
	for i := range peerCount {
		ip := make(net.IP, 4)
		copy(ip, peerBytes[i*6:i*6+4])
		resp.peers[i] = Peer {
			IP:   ip,
			Port: uint16(binary.BigEndian.Uint16(peerBytes[i*6+4:i*6+6])),
		}
	}

	if resp.action != ActionAnnounce {
		return nil, 0, fmt.Errorf("invalid action: %d", resp.action)
	}

	if resp.transactionID != announceTxID {
		return nil, 0, fmt.Errorf("invalid transaction ID: %d", resp.transactionID)
	}
	
	// return peers, interval
	return resp.peers, int(resp.interval), nil
}

// connectionExpired returns true if the connection has expired (more than a minute since creation) 55 seconds for avoiding edge cases
func (t *UDPTracker) connectionExpired() bool {
	return time.Since(t.connectionCreatedAt) > 55*time.Second
}

// Ensure the connection is not expired before sending an announce
func (t *UDPTracker) EnsureConnection() error {
	if t.conn == nil {
		_, err := t.Connect(t.addr)
		return err
	}
	if t.connectionExpired() {
		if t.conn != nil {
			t.conn.Close()
			t.conn = nil
			t.connectionID = 0
			t.connectionCreatedAt = time.Time{}
		}
		_, err := t.Connect(t.addr)
		return err
	}
	return nil
}

func (t *UDPTracker) Close() error {
    if t.conn != nil {
        err := t.conn.Close()
        t.conn = nil
        t.connectionID = 0
        t.connectionCreatedAt = time.Time{}
        return err
    }
    return nil
}