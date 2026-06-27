package main

import (
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"go-torrent/internal/peer"
	"go-torrent/internal/piece"
	"go-torrent/internal/torrent"
	"go-torrent/internal/tracker"
	"go-torrent/internal/tui"
)

// announceToTracker tries all tracker URLs in order, falling through on failure.
// It dispatches to HTTP or UDP based on each URL's scheme.
// When quiet is true, per-tracker diagnostics are not printed to stderr.
func announceToTracker(announceURLs []string, infoHash [20]byte, peerID [20]byte, port uint16, totalLength int64, quiet bool) ([]tracker.Peer, error) {
	if len(announceURLs) == 0 {
		return nil, fmt.Errorf("no tracker URLs available")
	}

	var lastErr error
	for i, announceURL := range announceURLs {
		parsed, err := url.Parse(announceURL)
		if err != nil {
			lastErr = fmt.Errorf("parsing %s: %w", announceURL, err)
			continue
		}

		var peers []tracker.Peer
		switch parsed.Scheme {
		case "http", "https":
			peers, err = tracker.AnnounceHTTP(announceURL, infoHash, peerID, port, totalLength)
		case "udp":
			peers, err = tracker.AnnounceUDP(parsed.Host, infoHash, peerID, port, totalLength)
		default:
			lastErr = fmt.Errorf("unsupported tracker scheme: %s (%s)", parsed.Scheme, announceURL)
			continue
		}
		if err == nil {
			if !quiet {
				fmt.Fprintf(os.Stderr, "  tracker %d/%d: %s — OK (%d peers)\n", i+1, len(announceURLs), announceURL, len(peers))
			}
			return peers, nil
		}
		lastErr = err
		if !quiet {
			fmt.Fprintf(os.Stderr, "  tracker %d/%d: %s — %v\n", i+1, len(announceURLs), announceURL, err)
		}
	}
	return nil, fmt.Errorf("all %d trackers failed; last error: %w", len(announceURLs), lastErr)
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

	var peers []tracker.Peer
	if len(tf.AnnounceList) > 0 {
		if !*tuiMode {
			fmt.Printf("Announcing to %d trackers...\n", len(tf.AnnounceList))
			fmt.Fprintf(os.Stderr, "  primary: %s\n", tf.Announce)
		}
		peers, err = announceToTracker(tf.AnnounceList, tf.InfoHash, peerID, uint16(*port), tf.Length, *tuiMode)
		if err != nil {
			if len(tf.URLList) == 0 && len(tf.HTTPSeeds) == 0 {
				return fmt.Errorf("announcing to tracker: %w", err)
			}
			if !*tuiMode {
				fmt.Fprintf(os.Stderr, "Warning: tracker announce failed; falling back to webseeds: %v\n", err)
			}
		}
	} else if len(tf.URLList) == 0 && len(tf.HTTPSeeds) == 0 {
		return fmt.Errorf("torrent has no tracker announce URL or webseed URL")
	}
	if !*tuiMode {
		fmt.Printf("Got %d peers from tracker\n", len(peers))
	}

	connections := peer.ConnectToPeers(&tf, peers)
	defer func() {
		for _, conn := range connections {
			conn.Close()
		}
	}()
	if !*tuiMode {
		fmt.Printf("Connected to %d peers\n", len(connections))
	}

	m := piece.NewManager(&tf, connections)
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
		return runTUI(m, statePath, outputPath)
	}

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

func runTUI(m *piece.Manager, statePath, outputPath string) error {
	m.SetLogWriter(io.Discard) // suppress stderr progress in TUI mode
	doneCh := make(chan error, 1)

	go func() {
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
