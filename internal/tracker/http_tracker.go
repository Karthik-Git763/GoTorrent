package tracker

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"go-torrent/internal/bencode"
)

type Peer struct {
	IP   net.IP
	Port uint16
}

func AnnounceHTTP(announceURL string, infoHash [20]byte, peerID [20]byte, port uint16, totalLength int64) ([]Peer, error) {
	parsedURL, err := url.Parse(announceURL)
	if err != nil {
		return nil, err
	}
	query := parsedURL.Query()
	query.Set("info_hash", escapeBytes(infoHash[:]))
	query.Set("peer_id", escapeBytes(peerID[:]))
	query.Set("port", fmt.Sprintf("%d", port))
	query.Set("uploaded", "0")
	query.Set("downloaded", "0")
	query.Set("left", fmt.Sprintf("%d", totalLength))
	query.Set("compact", "1")
	parsedURL.RawQuery = query.Encode()
	req, err := http.Get(parsedURL.String())
	if err != nil {
		return nil, err
	}
	defer req.Body.Close()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	return parseHTTPResponse(body)
}

func parseHTTPResponse(body []byte) ([]Peer, error) {
	decoded, _, err := bencode.Decode(body)
	if err != nil {
		return nil, err
	}
	top, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid tracker response: expected dict")
	}
	if failure, ok := top["failure reason"].(string); ok {
		return nil, fmt.Errorf("tracker failure: %s", failure)
	}

	peersVal, ok := top["peers"]
	if !ok {
		return nil, fmt.Errorf("invalid tracker response: missing peers")
	}

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

func escapeBytes(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_' || c == '~' {
			sb.WriteByte(c)
			continue
		}
		sb.WriteByte('%')
		sb.WriteByte("0123456789ABCDEF"[c>>4])
		sb.WriteByte("0123456789ABCDEF"[c&0x0F])
	}
	return sb.String()
}
