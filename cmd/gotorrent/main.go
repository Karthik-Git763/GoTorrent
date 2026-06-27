package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-torrent/internal/peer"
	"go-torrent/internal/piece"
	"go-torrent/internal/torrent"
	"go-torrent/internal/tracker"
	"go-torrent/internal/tui"
)

const trackerDiscoveryTimeout = 3 * time.Second

// announceToTracker announces to every tracker concurrently and combines their peers.
// When quiet is true, per-tracker diagnostics are not printed to stderr.
func announceToTracker(ctx context.Context, announceURLs []string, infoHash [20]byte, peerID [20]byte, port uint16, totalLength int64, quiet bool) ([]tracker.Peer, error) {
	peers, results, err := tracker.DiscoverPeers(ctx, announceURLs, infoHash, peerID, port, totalLength)
	if !quiet {
		for i, result := range results {
			if result.Err != nil {
				fmt.Fprintf(os.Stderr, "  tracker %d/%d: %s - %v\n", i+1, len(results), result.URL, result.Err)
				continue
			}
			fmt.Fprintf(os.Stderr, "  tracker %d/%d: %s - OK (%d peers)\n", i+1, len(results), result.URL, len(result.Peers))
		}
	}
	return peers, err
}

func run() error {
	port := flag.Int("port", 6881, "listen port for incoming connections")
	output := flag.String("output", "", "output directory (default: torrent name)")
	tuiMode := flag.Bool("tui", false, "terminal UI mode")
	flag.Parse()

	if flag.NArg() < 1 {
		return fmt.Errorf("Usage: gotorrent [flags] <torrent-file>\nFlags:\n  --port   listen port (default: 6881)\n  --output output directory\n  --tui    terminal UI mode")
	}

	torrentPath := flag.Arg(0)
	torrentData, err := os.ReadFile(torrentPath)
	if err != nil {
		return fmt.Errorf("reading torrent file: %w", err)
	}
	if !*tuiMode {
		fmt.Printf("Read %d bytes from %s\n", len(torrentData), torrentPath)
	}

	var tf torrent.TorrentFile
	if err := tf.Parse(torrentData); err != nil {
		return fmt.Errorf("parsing torrent file: %w", err)
	}

	peerID := peer.GeneratePeerID()
	outputPath := *output
	if outputPath == "" {
		outputPath = "."
	}

	m := piece.NewManager(&tf, nil)
	statePath := piece.StateFilePath(outputPath, tf.Name)

	// Try to resume from saved state
	if state, err := piece.LoadResume(statePath, tf.InfoHash); err == nil {
		completed := piece.CountCompleted(state.Completed)
		if !*tuiMode {
			fmt.Printf("Resuming from %d/%d completed pieces\n", completed, len(tf.PieceHashes))
		}
		m.SetCompleted(state.Completed)
	} else if !os.IsNotExist(err) && !*tuiMode {
		fmt.Printf("Warning: ignoring invalid resume state: %v\n", err)
	}

	m.EnablePeriodicSave(statePath, tf.InfoHash)

	if *tuiMode {
		return runTUIFlow(m, statePath, outputPath, &tf, peerID, uint16(*port))
	}

	var peers []tracker.Peer
	if len(tf.AnnounceList) > 0 {
		fmt.Printf("Announcing to %d trackers...\n", len(tf.AnnounceList))
		fmt.Fprintf(os.Stderr, "  primary: %s\n", tf.Announce)
		announceCtx, cancelAnnounce := context.WithTimeout(context.Background(), trackerDiscoveryTimeout)
		peers, err = announceToTracker(announceCtx, tf.AnnounceList, tf.InfoHash, peerID, uint16(*port), tf.Length, false)
		cancelAnnounce()
		if err != nil {
			if len(tf.URLList) == 0 && len(tf.HTTPSeeds) == 0 {
				return fmt.Errorf("announcing to tracker: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Warning: tracker announce failed; falling back to webseeds: %v\n", err)
		}
	} else if len(tf.URLList) == 0 && len(tf.HTTPSeeds) == 0 {
		return fmt.Errorf("torrent has no tracker announce URL or webseed URL")
	}
	fmt.Printf("Got %d peers from tracker\n", len(peers))

	connections, connectReport := peer.ConnectToPeersWithID(&tf, peers, peerID)
	defer func() {
		for _, conn := range connections {
			conn.Close()
		}
	}()
	fmt.Printf("Connected to %d peers\n", len(connections))
	if len(connections) == 0 && len(peers) > 0 {
		fmt.Fprintf(os.Stderr, "Peer connection summary: %d dialed, %d handshaken, %d usable\n",
			connectReport.Dialed, connectReport.Handshaken, len(connections))
	}

	m.SetPeers(connections)
	fmt.Printf("Progress %d/%d pieces (%.1f%%)\n",
		piece.CountCompleted(m.Completed()), len(tf.PieceHashes),
		float64(piece.CountCompleted(m.Completed()))/float64(len(tf.PieceHashes))*100)

	// Signal handler for graceful shutdown (CLI mode only)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nSaving state and exiting...")
		if err := piece.SaveResume(statePath, tf.InfoHash, m.Completed()); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving resume state: %v\n", err)
		} else {
			fmt.Printf("Resume state saved to %s\n", statePath)
		}
		os.Exit(0)
	}()

	if err := m.Download(outputPath, true); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	piece.RemoveResume(statePath)
	fmt.Println("Download complete!")
	return nil
}

func runTUIFlow(m *piece.Manager, statePath, outputPath string, tf *torrent.TorrentFile, peerID [20]byte, port uint16) error {
	m.SetLogWriter(io.Discard) // suppress stderr progress in TUI mode
	doneCh := make(chan error, 1)
	m.SetStatus("Connecting trackers...")

	go func() {
		var peers []tracker.Peer
		if len(tf.AnnounceList) > 0 {
			announceCtx, cancelAnnounce := context.WithTimeout(context.Background(), trackerDiscoveryTimeout)
			peers, _ = announceToTracker(announceCtx, tf.AnnounceList, tf.InfoHash, peerID, port, tf.Length, true)
			cancelAnnounce()
		}

		if len(peers) == 0 && len(tf.URLList) == 0 && len(tf.HTTPSeeds) == 0 {
			m.SetStatus("No peers or webseeds available")
			doneCh <- fmt.Errorf("no peers or webseeds available")
			return
		}

		m.SetStatus("Connecting peers...")
		connections, connectReport := peer.ConnectToPeersWithID(tf, peers, peerID)
		defer func() {
			for _, conn := range connections {
				conn.Close()
			}
		}()
		m.SetPeers(connections)

		if len(connections) == 0 && len(tf.URLList) == 0 && len(tf.HTTPSeeds) == 0 {
			m.SetStatus("No peers connected")
			doneCh <- fmt.Errorf("trackers returned %d peers, but none connected (%d dialed, %d completed handshake)",
				len(peers), connectReport.Dialed, connectReport.Handshaken)
			return
		}

		if len(connections) == 0 {
			m.SetStatus(fmt.Sprintf("Downloading with %d webseed(s)", len(tf.URLList)+len(tf.HTTPSeeds)))
		} else {
			m.SetStatus(fmt.Sprintf("Downloading with %d peer(s)", len(connections)))
		}

		err := m.Download(outputPath, true)
		if err == nil {
			piece.RemoveResume(statePath)
		}
		doneCh <- err
	}()

	return tui.Run(m, doneCh)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
