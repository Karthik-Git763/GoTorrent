package main

import (
	"flag"
	"fmt"
	"os"

	"go-torrent/internal/peer"
	"go-torrent/internal/piece"
	"go-torrent/internal/torrent"
	"go-torrent/internal/tracker"
)

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
	peers, err := tracker.AnnounceHTTP(tf.Announce, tf.InfoHash, peerID, uint16(*port), tf.Length)
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