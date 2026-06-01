package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: gotorrent <torrent-file>\n")
		os.Exit(1)
	}
	torrentFile, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading torrent file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Read %d bytes from %s\n", len(torrentFile), os.Args[1])
}
