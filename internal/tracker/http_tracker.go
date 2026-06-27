package tracker

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	"go-torrent/internal/bencode"
)

type Peer struct {
	IP   net.IP
	Port uint16
}

// AnnounceHTTP sends an HTTP announce request to the tracker and returns the list of peers.
func AnnounceHTTP(announceURL string, infoHash [20]byte, peerID [20]byte, port uint16, totalLength int64) ([]Peer, error) {
	// Parse the announce URL
	parsedURL, err := url.Parse(announceURL)
	if err != nil {
		return nil, err
	}
	// Set the query parameters
	query := parsedURL.Query()
	query.Set("info_hash", string(infoHash[:]))
	query.Set("peer_id", string(peerID[:]))
	query.Set("port", fmt.Sprintf("%d", port))
	query.Set("uploaded", "0")
	query.Set("downloaded", "0")
	query.Set("left", fmt.Sprintf("%d", totalLength))
	query.Set("compact", "1")
	// Encode the query parameters and set the URL
	parsedURL.RawQuery = query.Encode()
	req, err := http.Get(parsedURL.String())
	if err != nil {
		return nil, err
	}
	// Close the response body when done
	defer req.Body.Close()
	// Read the response body
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	// Parse the response body
	return parseHTTPResponse(body)
}

// parseHTTPResponse parses the HTTP response body and returns a list of peers.
func parseHTTPResponse(body []byte) ([]Peer, error) {
	// Decode the response body
	decoded, _, err := bencode.Decode(body)
	if err != nil {
		return nil, err
	}

	// Check the response type
	top, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid tracker response: expected dict")
	}
	if failure, ok := top["failure reason"].(string); ok {
		return nil, fmt.Errorf("tracker failure: %s", failure)
	}

	// Check for peers in the response
	peersVal, ok := top["peers"]
	if !ok {
		return nil, fmt.Errorf("invalid tracker response: missing peers")
	}

	// Unmarshal the peers based on their type
	switch peers := peersVal.(type) {
	case string:
		return unmarshalCompactPeers([]byte(peers))
	case []any:
		return unmarshalPeerList(peers)
	default:
		return nil, fmt.Errorf("invalid tracker response: unsupported peers type %T", peersVal)
	}
}

// unmarshalCompactPeers parses compact peer data (6 bytes per peer).
func unmarshalCompactPeers(peersBin []byte) ([]Peer, error) {
	const peerSize = 6
	if len(peersBin)%peerSize != 0 {
		return nil, fmt.Errorf("received malformed peers: length is not a multiple of %d", peerSize)
	}
	numPeers := len(peersBin) / peerSize
	peers := make([]Peer, numPeers)
	for i := range numPeers {
		offset := i * peerSize
		peers[i].IP = net.IP(peersBin[offset : offset+4])
		peers[i].Port = binary.BigEndian.Uint16(peersBin[offset+4 : offset+6])
	}
	return peers, nil
}

// unmarshalPeerList parses a non-compact peers list from a tracker response.
func unmarshalPeerList(peersList []any) ([]Peer, error) {
	peers := make([]Peer, 0, len(peersList))
	for _, entry := range peersList {
		peerMap, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid peers entry: expected dict")
		}
		ipStr, ok := peerMap["ip"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid peers entry: missing ip")
		}
		portVal, ok := peerMap["port"].(int64)
		if !ok {
			return nil, fmt.Errorf("invalid peers entry: missing port")
		}
		peers = append(peers, Peer{IP: net.ParseIP(ipStr), Port: uint16(portVal)})
	}
	return peers, nil
}
