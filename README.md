# GoTorrent

A BitTorrent client built from scratch in Go. Implements the core protocol stack — bencode, metadata parsing, tracker communication (HTTP + UDP), and peer wire protocol — with no framework dependencies beyond the Go standard library.

## What's Built

| Layer | Status |
|---|---|
| Bencode encoder/decoder | Done |
| Torrent file parser (single & multi-file) | Done |
| InfoHash computation (SHA1 of raw `info` dict) | Done |
| HTTP tracker announce (BEP 3) | Done |
| UDP tracker announce (BEP 15) | Done |
| Mock tracker server (offline testing) | Done |
| Peer wire protocol (handshake, messages) | Next |
| Piece download manager | Next |

## Architecture

```
cmd/gotorrent/          ─ CLI entry point
internal/piece/         ─ Piece download manager (in progress)
internal/peer/          ─ Peer wire protocol (in progress)
internal/tracker/       ─ HTTP + UDP tracker announce
internal/torrent/       ─ Torrent file parsing, infohash
internal/bencode/       ─ Recursive-descent bencode encoder/decoder
```

The bencode parser handles all wire types (integers, strings, lists, dicts, arbitrary nesting). The torrent parser extracts announce URLs, piece hashes, and computes the infohash from the raw bencoded info dict — the encoding isn't round-tripped through the decoded form, so the hash is guaranteed correct. The tracker layer supports both HTTP (compact and dictionary peer formats) and UDP (BEP 15 two-step connect/announce with auto-reconnection on connection expiry).

## Build & Run

```bash
go build ./cmd/gotorrent/
./gotorrent path/to/file.torrent
go test ./...
```

## Roadmap

- **Peer wire protocol** — Handshake, message types (choke/unchoke/bitfield/request/piece), goroutine-based concurrent reader/writer per peer connection
- **Piece download** — Work queue, rarest-first selection, SHA1 block verification, end-to-end download loop
- **Resume, rate limiting, TUI, magnet links** — Download state persistence, token-bucket rate control, Bubble Tea progress UI, BEP 9 metadata extension
- **Stretch** — DHT (BEP 5), PEX (BEP 11), uTP (BEP 29)