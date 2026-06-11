# GoTorrent

A BitTorrent client built from scratch in Go. Standard library only — every protocol byte is handled explicitly.

## Status

| Layer | Package | Status |
|-------|---------|--------|
| Bencode encoder/decoder | `internal/bencode/` | ✅ Done |
| Torrent file parser + infohash | `internal/torrent/` | ✅ Done |
| HTTP/UDP tracker announce | `internal/tracker/` | ✅ Done |
| Peer wire protocol | `internal/peer/` | ✅ Done |
| Piece download manager | `internal/piece/` | 🔜 Week 4 |

## Build

```bash
go build ./cmd/gotorrent/
go test ./... -v
./gotorrent path/to/file.torrent
```

## Architecture

```
.torrent → bencode.Decode → torrent.Parse → Infohash (SHA1)
    ↓
tracker.Announce(HTTP/UDP) → []Peer{IP, Port}
    ↓
peer.ConnectToPeers → TCP handshake → goroutine pair per peer
    ↓
piece.Manager → work queue → rarest-first → SHA1 verify → file
```

**Key design choices:**
- **Recursive-descent bencode parser** — cursor-based, returns unconsumed remainder
- **Infohash from raw bytes** — SHA1 of the raw `info` dict (not re-encoded — would change sort order)
- **Dual tracker** — HTTP (BEP 3) + UDP (BEP 15) with auto-reconnect on connection expiry
- **Goroutine pair per peer** — reader goroutine + writer goroutine communicating over buffered channels
- **`sync.Once`** idempotent Close, **`atomic.Bool`** lock-free choked state, **`sync.Cond`** writer park/unpark
- **`io.ReadFull` on every TCP read** — TCP streams; never trust `conn.Read()` for exact byte counts

## Roadmap

- **Week 4** — Piece download manager: work queue, rarest-first selection, SHA1 verification, download loop
- **Week 5** — Resume downloads, rate limiting, TUI (Bubble Tea), magnet links (BEP 9), end-game mode
- **Week 6** — Integration tests, error handling, graceful shutdown, race detection
- **Weeks 7-8** — DHT (BEP 5), PEX (BEP 11), uTP (BEP 29)
