package webseed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-torrent/internal/torrent"
)

func TestURLListSingleFileRange(t *testing.T) {
	file := []byte("0123456789")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movie.bin" {
			t.Fatalf("path = %q, want /movie.bin", r.URL.Path)
		}
		if got, want := r.Header.Get("Range"), "bytes=4-7"; got != want {
			t.Fatalf("Range = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Write(file[4:8])
	}))
	defer server.Close()

	tf := &torrent.TorrentFile{
		Name:        "movie.bin",
		Length:      int64(len(file)),
		PieceLength: 4,
		URLList:     []string{server.URL + "/"},
	}

	sources := NewSources(tf)
	if len(sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(sources))
	}
	data, err := sources[0].FetchPiece(context.Background(), 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "4567" {
		t.Fatalf("data = %q, want 4567", data)
	}
}

func TestURLListMultiFileSpanningPiece(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/root/bundle/a.txt":
			if got, want := r.Header.Get("Range"), "bytes=0-2"; got != want {
				t.Fatalf("Range for a.txt = %q, want %q", got, want)
			}
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("abc"))
		case "/root/bundle/dir/b.txt":
			if got, want := r.Header.Get("Range"), "bytes=0-1"; got != want {
				t.Fatalf("Range for b.txt = %q, want %q", got, want)
			}
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("de"))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	tf := &torrent.TorrentFile{
		Name:        "bundle",
		Length:      10,
		PieceLength: 5,
		Files: []torrent.FileEntry{
			{Length: 3, Path: []string{"a.txt"}},
			{Length: 7, Path: []string{"dir", "b.txt"}},
		},
		URLList: []string{server.URL + "/root/"},
	}

	data, err := NewSources(tf)[0].FetchPiece(context.Background(), 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abcde" {
		t.Fatalf("data = %q, want abcde", data)
	}
}

func TestHTTPSeedQuery(t *testing.T) {
	var infoHash [20]byte
	copy(infoHash[:], []byte("01234567890123456789"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Query().Get("piece"), "2"; got != want {
			t.Fatalf("piece query = %q, want %q", got, want)
		}
		if got := r.URL.Query().Get("info_hash"); got != string(infoHash[:]) {
			t.Fatalf("info_hash query = %q", got)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "piece-two")
	}))
	defer server.Close()

	tf := &torrent.TorrentFile{
		InfoHash:    infoHash,
		Length:      27,
		PieceLength: 9,
		HTTPSeeds:   []string{server.URL + "/seed"},
	}

	data, err := NewSources(tf)[0].FetchPiece(context.Background(), 2, 9)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "piece-two" {
		t.Fatalf("data = %q, want piece-two", data)
	}
}
