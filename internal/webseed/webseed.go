package webseed

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go-torrent/internal/torrent"
)

const requestTimeout = 30 * time.Second

var ErrUnavailable = fmt.Errorf("webseed unavailable")

// Source fetches complete torrent pieces over HTTP.
type Source interface {
	Name() string
	FetchPiece(ctx context.Context, index uint32, length int64) ([]byte, error)
	Disable()
	Available() bool
}

type baseSource struct {
	name         string
	client       *http.Client
	disabled     bool
	backoffUntil time.Time
	mu           sync.Mutex
}

func newBaseSource(name string) baseSource {
	return baseSource{
		name: name,
		client: &http.Client{
			Timeout: requestTimeout,
		},
	}
}

func (s *baseSource) Name() string { return s.name }

func (s *baseSource) Disable() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disabled = true
}

func (s *baseSource) Available() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.disabled && time.Now().After(s.backoffUntil)
}

func (s *baseSource) backoff(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backoffUntil = time.Now().Add(d)
}

func (s *baseSource) do(req *http.Request, want int64) ([]byte, error) {
	if !s.Available() {
		return nil, ErrUnavailable
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		s.backoff(parseRetryAfter(resp.Body))
		return nil, ErrUnavailable
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, want+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != want {
		return nil, fmt.Errorf("unexpected response length %d, want %d", len(data), want)
	}
	return data, nil
}

func parseRetryAfter(r io.Reader) time.Duration {
	body, _ := io.ReadAll(io.LimitReader(r, 32))
	var seconds int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(body)), "%d", &seconds); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 30 * time.Second
}

// NewSources builds all supported HTTP/HTTPS webseed sources from torrent metadata.
func NewSources(tf *torrent.TorrentFile) []Source {
	var sources []Source
	for _, raw := range tf.URLList {
		if isHTTP(raw) {
			sources = append(sources, newURLListSource(raw, tf))
		}
	}
	for _, raw := range tf.HTTPSeeds {
		if isHTTP(raw) {
			sources = append(sources, newHTTPSeedSource(raw, tf))
		}
	}
	return sources
}

func isHTTP(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

type urlListSource struct {
	baseSource
	root        string
	pieceLen    int64
	totalLength int64
	name        string
	files       []torrent.FileEntry
	offsets     []int64
}

func newURLListSource(raw string, tf *torrent.TorrentFile) *urlListSource {
	offsets := make([]int64, len(tf.Files))
	var total int64
	for i, f := range tf.Files {
		offsets[i] = total
		total += f.Length
	}
	return &urlListSource{
		baseSource:  newBaseSource(raw),
		root:        raw,
		pieceLen:    tf.PieceLength,
		totalLength: tf.Length,
		name:        tf.Name,
		files:       tf.Files,
		offsets:     offsets,
	}
}

func (s *urlListSource) FetchPiece(ctx context.Context, index uint32, length int64) ([]byte, error) {
	pieceOffset := int64(index) * s.pieceLen
	if pieceOffset < 0 || pieceOffset+length > s.totalLength {
		return nil, fmt.Errorf("piece %d outside torrent bounds", index)
	}

	if len(s.files) == 0 {
		fileURL := s.singleFileURL()
		return s.fetchRange(ctx, fileURL, pieceOffset, length)
	}

	out := make([]byte, 0, length)
	for i, entry := range s.files {
		fileStart := s.offsets[i]
		fileEnd := fileStart + entry.Length
		overlapStart := max(pieceOffset, fileStart)
		overlapEnd := min(pieceOffset+length, fileEnd)
		if overlapStart >= overlapEnd {
			continue
		}
		part, err := s.fetchRange(ctx, s.multiFileURL(entry.Path), overlapStart-fileStart, overlapEnd-overlapStart)
		if err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	if int64(len(out)) != length {
		return nil, fmt.Errorf("assembled %d bytes, want %d", len(out), length)
	}
	return out, nil
}

func (s *urlListSource) fetchRange(ctx context.Context, raw string, start, length int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, start+length-1))
	return s.do(req, length)
}

func (s *urlListSource) singleFileURL() string {
	if strings.HasSuffix(s.root, "/") {
		return appendEscapedPath(s.root, s.name)
	}
	return s.root
}

func (s *urlListSource) multiFileURL(parts []string) string {
	all := append([]string{s.name}, parts...)
	return appendEscapedPath(s.root, all...)
}

type httpSeedSource struct {
	baseSource
	raw string
	tf  *torrent.TorrentFile
}

func newHTTPSeedSource(raw string, tf *torrent.TorrentFile) *httpSeedSource {
	return &httpSeedSource{
		baseSource: newBaseSource(raw),
		raw:        raw,
		tf:         tf,
	}
}

func (s *httpSeedSource) FetchPiece(ctx context.Context, index uint32, length int64) ([]byte, error) {
	u, err := url.Parse(s.raw)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("info_hash", string(s.tf.InfoHash[:]))
	q.Set("piece", fmt.Sprintf("%d", index))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	return s.do(req, length)
}

func appendEscapedPath(raw string, parts ...string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	escaped := make([]string, 0, len(parts))
	for _, part := range parts {
		escaped = append(escaped, url.PathEscape(part))
	}
	suffix := strings.Join(escaped, "/")
	if suffix == "" {
		return u.String()
	}
	basePath := strings.TrimRight(u.Path, "/")
	baseRawPath := strings.TrimRight(u.EscapedPath(), "/")
	unescapedSuffix := strings.Join(parts, "/")
	u.Path = basePath + "/" + unescapedSuffix
	u.RawPath = baseRawPath + "/" + suffix
	return u.String()
}
