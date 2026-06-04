package tracker

import (
	"encoding/binary"
	"go-torrent/internal/bencode"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// mock_tracker is a mock implementation of the Tracker interface for testing purposes.

// MockTrackerServer is a test HTTP tracker that returns a controlled response.
type MockTrackerServer struct {
	Server *httptest.Server
	URL string
	// Configurable responses
	Peers []Peer
	Interval int
	// Error injection
	FailNext bool
	FailReason string
	// Spy: Record what was called
	LastInfoHash [20]byte
	LastPeerID [20]byte
	LastPort int
	RawQuery string
	Calls int
}

// NewMockTrackerServer creates a new MockTrackerServer.
func NewMockTrackerServer(t *testing.T) *MockTrackerServer {
	m := &MockTrackerServer{
		Peers: []Peer{
			{IP: net.ParseIP("127.0.0.1"), Port: 6881},
			{IP: net.ParseIP("127.0.0.1"), Port: 6882},
		},
		Interval: 1800,
	}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	m.URL = m.Server.URL + "/announce"
	t.Cleanup(m.Server.Close)
	return m
}

func (m *MockTrackerServer) handle(w http.ResponseWriter, r *http.Request) {
	m.Calls++

	// Record query parameters (spy)
	infoHash := r.URL.Query().Get("info_hash")
	copy(m.LastInfoHash[:], []byte(infoHash))
	peerID := r.URL.Query().Get("peer_id")
	copy(m.LastPeerID[:], []byte(peerID))
	port := r.URL.Query().Get("port")
	m.LastPort, _ = strconv.Atoi(port)
	m.RawQuery = r.URL.RawQuery

	if m.FailNext {
		m.FailNext = false
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(m.FailReason))
		return
	}
	if m.FailReason != "" {
		resp, _ := bencode.Marshal(map[string]any{
			"failure reason": m.FailReason,
		})
		w.Write(resp)
		return
	}

	// Build compact peer binary
	peersBin := make([]byte, len(m.Peers)*6)
	for i, peer := range m.Peers {
		copy(peersBin[i*6:], peer.IP.To4())
		binary.BigEndian.PutUint16(peersBin[i*6+4:], peer.Port)
	}

	resp, _ := bencode.Marshal(map[string]any{
		"complete": len(m.Peers),
		"incomplete": 0,
		"interval": m.Interval,
		"peers":    peersBin,
	})
	_, err := w.Write(resp)
	if err != nil {
		return
	}
}