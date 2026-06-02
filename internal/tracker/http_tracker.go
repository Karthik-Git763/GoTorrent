package tracker

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type Peer struct {
	IP   string
	Port int
}

func AnnounceHTTP(announceURL string, infoHash [20]byte, peerID [20]byte, port uint16, totalLength int64) ([]Peer, error) {
	parsedURL, err := url.Parse(announceURL)
	if err != nil {
		return nil, err
	}
	query := parsedURL.Query()
	query.Set("info_hash", string(infoHash[:]))
	query.Set("peer_id", string(peerID[:]))
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
	peers := make([]Peer, 0)
	
	for i := 0; i < len(body); i += 6 {
		peers = append(peers, Peer{
			IP:   string(body[i : i+4]),
			Port: int(body[i+4])<<8 | int(body[i+5]),
		})
	}
	return peers, nil
}
