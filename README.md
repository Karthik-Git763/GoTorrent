# GoTorrent

A BitTorrent client built from scratch in Go. Standard library only — every protocol byte is handled explicitly. No frameworks, no external dependencies, just the wire protocol and Go's concurrency primitives.

## Quick Start

```bash
# Build
go build ./cmd/gotorrent/

# CLI mode — simple progress output
./gotorrent --output /downloads path/to/file.torrent

# TUI mode — real-time terminal UI with progress bar, piece map, peers
./gotorrent --tui --output /downloads path/to/file.torrent

# Resume an interrupted download (auto-detected in both modes)
./gotorrent --output /downloads path/to/file.torrent
```

Download state persists across runs — Ctrl+C saves progress, re-running picks up where you left off.

## Architecture

```
.torrent
   ↓
bencode.Decode → torrent.Parse → InfoHash (SHA1 of raw info dict)
   ↓
tracker.Announce (HTTP BEP 3 / UDP BEP 15) → []Peer
   ↓
peer.ConnectToPeers → TCP handshake → bitfield → interested
   ↓
piece.Manager → per-peer goroutines → rarest-first selection → endgame → SHA1 verify → write
```

### Package Map

| Package | Responsibility |
|---------|---------------|
| `internal/bencode/` | Recursive-descent decoder + encoder. Cursor-based, returns unconsumed remainder. |
| `internal/torrent/` | Torrent file parser. Computes infohash from raw `info` bytes (no re-encode — preserves sort order). |
| `internal/tracker/` | HTTP (BEP 3) + UDP (BEP 15) tracker announces. Auto-reconnect on connection expiry. |
| `internal/peer/` | TCP connect, BitTorrent handshake, wire protocol (choke/unchoke/interested/request/piece/have/bitfield). Goroutine pair per peer (reader + writer) over buffered channels. |
| `internal/piece/` | Download orchestration. Per-peer worker goroutines with independent piece selection. |

### How Downloads Work

Each connected peer gets its own worker goroutine with a reader/writer pair. Workers independently select which piece to download next using a two-tier strategy:

1. **Rarest-first** — picks the piece with the fewest copies across all peers. Distributes load and preserves rare pieces.
2. **First-piece fallback** — when all fresh pieces are claimed, picks any piece the peer has (even if another peer is already downloading it). This is the **endgame** — the last few pieces are requested from multiple peers simultaneously, eliminating slow-tail latency.

Once a piece is selected, the worker pipelines all block requests (16 KiB each) to the peer, collects responses into a local buffer, verifies the SHA1 hash, and sends the verified result to the collector goroutine.

### Upload / Seeding

Peers are not just downloaders — they also upload. When a remote peer requests a block we have completed, the writer goroutine reads the piece data from disk and sends a piece response over the same TCP connection. Completed pieces are announced via Have messages to all connected peers, and our bitfield is broadcast at connection start so peers know what we can serve.

On receiving an Interested message from a peer, we immediately Unchoke them — no complex unchoking algorithm. This makes us a cooperative leecher/seeder, improving our download speed as peers are more willing to reciprocate.

### Resume

State is saved to `<output>/<torrent-name>.gtstate` as JSON:
- Every 10 completed pieces during download
- On SIGINT/SIGTERM (Ctrl+C)
- Validated by infohash on reload — mismatched state is discarded

On resume, the output file is opened without truncation and pre-allocated to the torrent's full size. Already-completed pieces are skipped; workers only download what's missing.

## Key Design Choices

- **Per-worker piece selection** — no shared work queue. Workers always pick pieces their peer actually has, eliminating re-queue churn.
- **Worker-local buffers** — each worker assembles pieces in its own `[]byte`. No shared `pendingPieces` map, no races.
- **Per-peer goroutine pair** — a dedicated reader goroutine deserialises wire messages into a buffered channel; a dedicated writer goroutine drains a request queue. Choke/unchoke state is an `atomic.Bool` with a `sync.Cond` for parking the writer.
- **`io.ReadFull` on every TCP read** — TCP is a stream; `conn.Read()` may return partial data. Always read exact byte counts.
- **`net.JoinHostPort` for addresses** — works with both IPv4 and IPv6.
- **Context cancellation for shutdown** — `context.CancelFunc` tears down goroutines cleanly. Channels are never closed — goroutines exit via `ctx.Done()` select cases.
- **Results channel, not shared state** — workers send results via `chan PieceResult`; the collector goroutine is the exclusive writer of the `completed` bitfield. No mutex contention on the hot path.

## Tests

```bash
# All tests, with race detector
go test ./... -race -count=1
```

The project uses table-driven subtests, `net.Pipe()` for in-memory protocol testing, and mock TCP listeners for integration tests.

## Stretch Goals

- **DHT (BEP 5)** — Distributed hash table for trackerless peer discovery
- **PEX (BEP 11)** — Peer exchange to share peer lists between connected peers
- **Magnet links (BEP 9)** — Download without a `.torrent` file, using just an infohash
- **uTP (BEP 29)** — UDP-based transport with latency-aware congestion control
- **Rate limiting** — Per-peer and global bandwidth caps
- **WebSeed (BEP 17/19)** — Download pieces over HTTP from static file hosts
- **v2 torrents (BEP 52)** — Support for the BitTorrent v2 spec (SHA-256 hashes, merkle trees)