package tracker

import (
	"context"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-torrent/internal/bencode"
)

func TestDiscoverPeersAnnouncesConcurrentlyAndDeduplicates(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})

	serverOne := newDiscoveryTracker(t, started, release, []Peer{
		{IP: net.IPv4(127, 0, 0, 1), Port: 6881},
	})
	serverTwo := newDiscoveryTracker(t, started, release, []Peer{
		{IP: net.IPv4(127, 0, 0, 1), Port: 6881},
		{IP: net.IPv4(127, 0, 0, 2), Port: 6882},
	})

	type discoveryResult struct {
		peers   []Peer
		results []AnnounceResult
		err     error
	}
	done := make(chan discoveryResult, 1)
	go func() {
		peers, results, err := DiscoverPeers(
			context.Background(),
			[]string{serverOne.URL, serverTwo.URL},
			[20]byte{}, [20]byte{}, 6881, 1024,
		)
		done <- discoveryResult{peers: peers, results: results, err: err}
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("trackers were not announced to concurrently")
		}
	}
	close(release)

	result := <-done
	if result.err != nil {
		t.Fatalf("DiscoverPeers() error = %v", result.err)
	}
	if len(result.results) != 2 {
		t.Fatalf("got %d announce results, want 2", len(result.results))
	}
	if len(result.peers) != 2 {
		t.Fatalf("got %d unique peers, want 2", len(result.peers))
	}
}

func TestDiscoverPeersHonorsDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, results, err := DiscoverPeers(ctx, []string{server.URL}, [20]byte{}, [20]byte{}, 6881, 1024)

	if err == nil {
		t.Fatal("DiscoverPeers() error = nil, want deadline error")
	}
	if len(results) != 1 || results[0].Err == nil {
		t.Fatalf("results = %#v, want one failed announce", results)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("deadline cancellation took %s", elapsed)
	}
}

func newDiscoveryTracker(t *testing.T, started chan<- struct{}, release <-chan struct{}, peers []Peer) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release

		compact := make([]byte, len(peers)*6)
		for i, peer := range peers {
			copy(compact[i*6:i*6+4], peer.IP.To4())
			binary.BigEndian.PutUint16(compact[i*6+4:i*6+6], peer.Port)
		}
		body, err := bencode.Marshal(map[string]any{"peers": compact})
		if err != nil {
			t.Errorf("encoding tracker response: %v", err)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}
