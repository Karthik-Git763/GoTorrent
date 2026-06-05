package tracker

import (
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Later for Mock refactor
// type MockUDPTracker struct {
// 	Conn *net.UDPConn
// 	ConnectionCalls int
// 	AnnounceCalls int
// 	Peers []Peer
// 	Interval int
// 	FailConnect bool
// 	FailAnnounce bool
// }

// TestAnnounceHTTP_MockTracker tests the AnnounceHTTP function with a mock tracker server.
func TestAnnounceHTTP_MockTracker(t *testing.T) {
    mock := NewMockTrackerServer(t)

    var infoHash, peerID [20]byte
    infoHash = [20]byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9, 0xa, 0xb, 0xc, 0xd, 0xe, 0xf, 0x10, 0x11, 0x12, 0x13, 0x14}
    peerID = [20]byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9, 0xa, 0xb, 0xc, 0xd, 0xe, 0xf, 0x10, 0x11, 0x12, 0x13, 0x14}
    
    peers, err := AnnounceHTTP(mock.URL, infoHash, peerID, 6881, 1000)
    if err != nil {
        t.Fatal(err)
    }
    if len(peers) != 2 {
        t.Fatalf("expected 2 peers, got %d", len(peers))
    }
    if peers[0].IP.String() != "127.0.0.1" {
        t.Errorf("expected 127.0.0.1, got %s", peers[0].IP)
    }
    if peers[0].Port != 6881 {
        t.Errorf("expected port 6881, got %d", peers[0].Port)
    }
    if peers[1].IP.String() != "127.0.0.1" {
        t.Errorf("expected 127.0.0.1, got %s", peers[1].IP)
    }
    if peers[1].Port != 6882 {
        t.Errorf("expected port 6882, got %d", peers[1].Port)
    }

    if mock.Calls != 1 {
        t.Errorf("expected 1 call, got %d", mock.Calls)
    }
    if mock.LastPort != 6881 {
        t.Errorf("expected port 6881, got %d", mock.LastPort)
    }
}

// TestAnnounceHTTP_TrackerFailure tests the AnnounceHTTP function with a mock tracker server that fails.
func TestAnnounceHTTP_TrackerFailure(t *testing.T) {
    mock := NewMockTrackerServer(t)
    mock.FailReason = "torrent not found"

    var infoHash, peerID [20]byte
    _, err := AnnounceHTTP(mock.URL, infoHash, peerID, 6881, 1000)
    if err == nil || !strings.Contains(err.Error(), "torrent not found") {
        t.Fatalf("expected failure reason error, got: %v", err)
    }
}

// TestAnnounceHTTP_MalformedResponse tests the AnnounceHTTP function with a mock tracker server that returns malformed data.
func TestAnnounceHTTP_MalformedResponse(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("not bencoded data"))
    }))
    defer server.Close()

    var infoHash, peerID [20]byte
    _, err := AnnounceHTTP(server.URL, infoHash, peerID, 6881, 1000)
    if err == nil {
        t.Fatal("expected error for malformed response")
    }
}

// TestAnnounceHTTP_HTTP500 tests the AnnounceHTTP function with a mock tracker server that returns a 500 status code.
func TestAnnounceHTTP_HTTP500(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    	w.WriteHeader(http.StatusInternalServerError)
    }))
    defer server.Close()
    var infoHash, peerID [20]byte
    _, err := AnnounceHTTP(server.URL, infoHash, peerID, 6881, 1000)
    if err == nil {
        t.Fatal("expected error for HTTP 500 response")
    }
}

// TestAnnounceUDP tests the AnnounceUDP function with a mock tracker server.
func TestAnnounceUDP(t *testing.T) {
    addr, _ := net.ResolveUDPAddr(
    	"udp",
    	"127.0.0.1:0", // 0 Port means OS picks free port
    )
    conn, _ := net.ListenUDP("udp", addr)
    defer conn.Close()
    trackerAddr := conn.LocalAddr().(*net.UDPAddr) // Get the local address of the tracker which client will use to connect
    go func() {
        buf := make([]byte, 2048)
        for {
            n, clientAddr, err := conn.ReadFromUDP(buf)
            if err != nil {
                return
            }
            // Connect Request
            if n >= 16 {
                action := binary.BigEndian.Uint32(buf[8:12])
                switch action {
                case ActionConnect:
                    transactionID := binary.BigEndian.Uint32(buf[12:16])

                    // Connect response
                    resp := make([]byte, 16)
                    binary.BigEndian.PutUint32(resp[0:4], ActionConnect) // action
                    binary.BigEndian.PutUint32(resp[4:8], transactionID) // transaction ID
                    binary.BigEndian.PutUint64(resp[8:16], 123456789) // connection ID
                    conn.WriteToUDP(resp, clientAddr)
                
                // Announce Request
                case ActionAnnounce:
                    transactionID := binary.BigEndian.Uint32(buf[12:16])

                    // Announce Response
                    resp := make([]byte, 32)
                    binary.BigEndian.PutUint32(resp[0:4], ActionAnnounce) // action
                    binary.BigEndian.PutUint32(resp[4:8], transactionID) // transaction ID
                    binary.BigEndian.PutUint32(resp[8:12], 1800) // interval
                    binary.BigEndian.PutUint32(resp[12:16], 0) // leechers
                    binary.BigEndian.PutUint32(resp[16:20], 2) // seeders
                    
                    copy(resp[20:24], net.IPv4(127,0, 0, 1).To4()) // IP
                    binary.BigEndian.PutUint16(resp[24:26], 6881) // Port
                    
                    copy(resp[26:30], net.IPv4(127, 0, 0, 1).To4()) // IP
                    binary.BigEndian.PutUint16(resp[30:32], 6882) // Port

                    _, err := conn.WriteToUDP(resp, clientAddr)
                    if err != nil {
                        return
                    }
                }
            }
        }
    }()

    // Client connects to the UDP tracker
    tracker := &UDPTracker{}
    defer tracker.Close()
    connID, err := tracker.Connect(trackerAddr)
    if err != nil {
        t.Fatal(err)
    }
    if connID != 123456789 {
        t.Fatalf("Unexpected connection ID: %d, expected 123456789", connID)
    }

    var infoHash [20]byte
    var peerID [20]byte

    peer, interval, err := tracker.Announce(infoHash, peerID, 6881, 1000)
    if err != nil {
        t.Fatal(err)
    }

    if interval != 1800 {
        t.Fatalf("Unexpected interval: %d, expected 1800", interval)
    }
    if len(peer) != 2 {
        t.Fatalf("Unexpected peer: %d, expected 2 peers", len(peer))
    }
    if peer[0].Port != 6881 {
        t.Fatalf("Unexpected peer port: %d, expected 6881", peer[0].Port)
    }
    if peer[1].Port != 6882 {
        t.Fatalf("Unexpected peer port: %d, expected 6882", peer[1].Port)
    }
    if peer[0].IP.String() != "127.0.0.1" {
        t.Fatalf("Unexpected peer IP: %s, expected 127.0.0.1", peer[0].IP)
    }
    if peer[1].IP.String() != "127.0.0.1" {
        t.Fatalf("Unexpected peer IP: %s, expected 127.0.0.1", peer[1].IP)
    }
}