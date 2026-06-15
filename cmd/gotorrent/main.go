package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	"go-torrent/internal/peer"
	"go-torrent/internal/piece"
	"go-torrent/internal/torrent"
	"go-torrent/internal/tracker"
)

// announceToTracker sends the announce request to the tracker, dispatching
// to HTTP or UDP based on the announce URL scheme.
func announceToTracker(announceURL string, infoHash [20]byte, peerID [20]byte, port uint16, totalLength int64) ([]tracker.Peer, error) {
	parsed, err := url.Parse(announceURL)
	if err != nil {
		return nil, fmt.Errorf("parsing announce URL: %w", err)
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return tracker.AnnounceHTTP(announceURL, infoHash, peerID, port, totalLength)
	case "udp":
		// net.ResolveUDPAddr expects "host:port", not full URL
		host := parsed.Host
		return tracker.AnnounceUDP(host, infoHash, peerID, port, totalLength)
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", parsed.Scheme)
	}
}

func main() {
	port := flag.Int("port", 6881, "listen port for incoming connections")
	output := flag.String("output", "", "output directory (default: torrent name)")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: gotorrent [flags] <torrent-file>\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	torrentPath := flag.Arg(0)

	torrentData, err := os.ReadFile(torrentPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading torrent file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Read %d bytes from %s\n", len(torrentData), torrentPath)

	var tf torrent.TorrentFile
	if err := tf.Parse(torrentData); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing torrent file: %v\n", err)
		os.Exit(1)
	}

	peerID := peer.GeneratePeerID()
	outputPath := *output
	if outputPath == "" {
		outputPath = "."
	}

	fmt.Printf("Announcing to tracker: %s\n", tf.Announce)
		peers, err := announceToTracker(tf.Announce, tf.InfoHash, peerID, uint16(*port), tf.Length)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error announcing to tracker: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Got %d peers from tracker\n", len(peers))

	connections := peer.ConnectToPeers(&tf, peers)
	defer func() {
		for _, conn := range connections {
			conn.Close()
		}
	}()
	fmt.Printf("Connected to %d peers\n", len(connections))

	m := piece.NewManager(&tf, connections)
	if err := m.Download(outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Download complete!")
}