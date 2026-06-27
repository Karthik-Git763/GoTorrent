package tracker

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
)

const defaultAnnounceTimeout = 5 * time.Second

// AnnounceResult records the outcome of one tracker announce.
type AnnounceResult struct {
	URL   string
	Peers []Peer
	Err   error
}

// DiscoverPeers announces to all trackers concurrently and returns the unique
// peers reported by every successful tracker. The caller controls the maximum
// discovery duration through ctx.
func DiscoverPeers(ctx context.Context, announceURLs []string, infoHash [20]byte, peerID [20]byte, port uint16, totalLength int64) ([]Peer, []AnnounceResult, error) {
	if len(announceURLs) == 0 {
		return nil, nil, fmt.Errorf("no tracker URLs available")
	}

	type indexedResult struct {
		index  int
		result AnnounceResult
	}

	resultsCh := make(chan indexedResult, len(announceURLs))
	for i, announceURL := range announceURLs {
		go func() {
			peers, err := announceOne(ctx, announceURL, infoHash, peerID, port, totalLength)
			resultsCh <- indexedResult{
				index:  i,
				result: AnnounceResult{URL: announceURL, Peers: peers, Err: err},
			}
		}()
	}

	results := make([]AnnounceResult, len(announceURLs))
	for range announceURLs {
		result := <-resultsCh
		results[result.index] = result.result
	}

	seen := make(map[string]struct{})
	var peers []Peer
	var announceErrors []error
	successes := 0
	for _, result := range results {
		if result.Err != nil {
			announceErrors = append(announceErrors, fmt.Errorf("%s: %w", result.URL, result.Err))
			continue
		}
		successes++
		for _, peer := range result.Peers {
			key := fmt.Sprintf("%s:%d", peer.IP.String(), peer.Port)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			peers = append(peers, peer)
		}
	}

	if successes == 0 {
		return nil, results, fmt.Errorf("all %d trackers failed: %w", len(announceURLs), errors.Join(announceErrors...))
	}
	return peers, results, nil
}

func announceOne(ctx context.Context, announceURL string, infoHash [20]byte, peerID [20]byte, port uint16, totalLength int64) ([]Peer, error) {
	parsed, err := url.Parse(announceURL)
	if err != nil {
		return nil, fmt.Errorf("parsing tracker URL: %w", err)
	}

	switch parsed.Scheme {
	case "http", "https":
		return AnnounceHTTPContext(ctx, announceURL, infoHash, peerID, port, totalLength)
	case "udp":
		return AnnounceUDPContext(ctx, parsed.Host, infoHash, peerID, port, totalLength)
	default:
		return nil, fmt.Errorf("unsupported tracker scheme %q", parsed.Scheme)
	}
}
