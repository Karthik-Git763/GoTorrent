package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: gotorrent <command>")
		os.Exit(1)
	}
	torrent, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Read %d bytes from %s\n", len(torrent), os.Args[1])
}