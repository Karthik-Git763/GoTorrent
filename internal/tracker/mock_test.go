package tracker

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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