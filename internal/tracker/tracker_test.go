package tracker

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"go-torrent/internal/torrent"
)

func TestAnnounceHTTPWithTorrentFile(t *testing.T) {
	const torrentPath = "testdata/big-buck-bunny.torrent"
	const peerIDHex = "2d474f303030312d313233343536373839303132" // "-GO0001-123456789012"

	data, err := os.ReadFile(torrentPath)
	if err != nil {
		t.Skipf("missing %s: %v", torrentPath, err)
	}

	var tf torrent.TorrentFile
	if err := tf.Parse(data); err != nil {
		t.Fatalf("failed to parse torrent: %v", err)
	}

	peerIDBytes, err := hex.DecodeString(peerIDHex)
	if err != nil || len(peerIDBytes) != 20 {
		t.Fatalf("peerIDHex must be 40 hex chars (20 bytes), got %q", peerIDHex)
	}
	var peerID [20]byte
	copy(peerID[:], peerIDBytes)

	trackers := []string{
		"http://tracker.opentrackr.org:1337/announce",
		"http://tracker.openbittorrent.com:80/announce",
		"http://tracker.publicbt.com:80/announce",
	}
	if trackerOverride := os.Getenv("TRACKER_URL"); trackerOverride != "" {
		trackers = []string{trackerOverride}
	}

	var lastErr error
	for _, trackerURL := range trackers {
		peers, err := AnnounceHTTP(trackerURL, tf.InfoHash, peerID, 6881, tf.Length)
		if err != nil {
			if isNetworkError(err) || isNonBencodedResponse(err) {
				lastErr = err
				continue
			}
			t.Fatalf("announce failed: %v", err)
		}
		if len(peers) == 0 {
			lastErr = fmt.Errorf("expected peers, got none from %s", trackerURL)
			continue
		}
		return
	}

	if lastErr != nil {
		t.Skipf("no reachable tracker: %v", lastErr)
	}
	t.Skip("no trackers configured")
}

func isNetworkError(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

func isNonBencodedResponse(err error) bool {
	return strings.Contains(err.Error(), "unknown type: <")
}
