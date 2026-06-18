package torrent

import (
	"fmt"

	"go-torrent/internal/bencode"
)

// FileEntry represents a single file in a multi-file torrent.
type FileEntry struct {
	Length int64
	Path   []string // path components relative to the torrent name
}

type TorrentFile struct {
	Announce     string
	AnnounceList []string // all tracker URLs (including the primary), for fallback
	InfoHash     [20]byte
	PieceHashes  [][20]byte
	PieceLength  int64
	Length       int64
	Name         string
	Files        []FileEntry
}

func (tf *TorrentFile) Parse(rawTorrent []byte) error {
	decoded, _, err := bencode.Decode(rawTorrent)
	if err != nil {
		return err
	}

	top, ok := decoded.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid torrent: expected top-level dict")
	}

	tf.Announce, tf.AnnounceList = extractAnnounce(top)

	infoRaw, infoMap, err := extractInfo(rawTorrent)
	if err != nil {
		return err
	}
	tf.computeInfoHash(infoRaw)

	pieceLength, ok := infoMap["piece length"].(int64)
	if !ok {
		return fmt.Errorf("invalid torrent: missing piece length")
	}
	length, ok := infoMap["length"].(int64)
	if !ok {
		filesVal, hasFiles := infoMap["files"].([]any)
		if !hasFiles {
			return fmt.Errorf("invalid torrent: missing length")
		}
		filesLength, err := sumFilesLength(filesVal)
		if err != nil {
			return err
		}
		length = filesLength

		entries, err := parseFileEntries(filesVal)
		if err != nil {
			return err
		}
		tf.Files = entries
	}
	name, ok := infoMap["name"].(string)
	if !ok {
		return fmt.Errorf("invalid torrent: missing name")
	}
	piecesStr, ok := infoMap["pieces"].(string)
	if !ok {
		return fmt.Errorf("invalid torrent: missing pieces")
	}

	pieceHashes, err := splitPieceHashes([]byte(piecesStr))
	if err != nil {
		return err
	}

	tf.PieceLength = pieceLength
	tf.Length = length
	tf.Name = name
	tf.PieceHashes = pieceHashes

	return nil
}

// extractAnnounce tries announce → announce-list to find tracker URLs.
// url-list (BEP 19 webseeds) is not used — those are file mirrors, not trackers.
// Returns the primary announce URL and the full deduplicated list of all tracker URLs.
func extractAnnounce(top map[string]any) (string, []string) {
	announce := ""
	if a, ok := top["announce"].(string); ok && a != "" {
		announce = a
	}

	// Collect from announce-list (BEP 12)
	seen := make(map[string]bool)
	var all []string
	if list, ok := top["announce-list"].([]any); ok {
		for _, tier := range list {
			group, ok := tier.([]any)
			if !ok {
				continue
			}
			for _, entry := range group {
				s, ok := entry.(string)
				if !ok || s == "" || seen[s] {
					continue
				}
				seen[s] = true
				all = append(all, s)
			}
		}
	}

	// Ensure primary announce is first in the list
	if announce != "" && !seen[announce] {
		all = append([]string{announce}, all...)
	}

	return announce, all
}

func extractInfo(rawTorrent []byte) ([]byte, map[string]any, error) {
	if len(rawTorrent) == 0 || rawTorrent[0] != 'd' {
		return nil, nil, fmt.Errorf("invalid torrent: expected dict")
	}
	idx := 1
	for {
		if idx >= len(rawTorrent) {
			return nil, nil, fmt.Errorf("invalid torrent: missing end 'e'")
		}
		if rawTorrent[idx] == 'e' {
			break
		}

		keyVal, rest, err := bencode.Decode(rawTorrent[idx:])
		if err != nil {
			return nil, nil, err
		}
		key, ok := keyVal.(string)
		if !ok {
			return nil, nil, fmt.Errorf("invalid torrent: non-string key")
		}
		consumedKey := len(rawTorrent[idx:]) - len(rest)
		idx += consumedKey

		valueStart := idx
		valueVal, rest, err := bencode.Decode(rawTorrent[idx:])
		if err != nil {
			return nil, nil, err
		}
		consumedVal := len(rawTorrent[idx:]) - len(rest)
		idx += consumedVal

		if key == "info" {
			infoMap, ok := valueVal.(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("invalid torrent: info is not a dict")
			}
			return rawTorrent[valueStart : valueStart+consumedVal], infoMap, nil
		}
	}

	return nil, nil, fmt.Errorf("invalid torrent: missing info dict")
}

func sumFilesLength(files []any) (int64, error) {
	var total int64
	for _, entry := range files {
		fileMap, ok := entry.(map[string]any)
		if !ok {
			return 0, fmt.Errorf("invalid torrent: files entry is not a dict")
		}
		lengthVal, ok := fileMap["length"].(int64)
		if !ok {
			return 0, fmt.Errorf("invalid torrent: file entry missing length")
		}
		total += lengthVal
	}
	return total, nil
}

func parseFileEntries(files []any) ([]FileEntry, error) {
	entries := make([]FileEntry, 0, len(files))
	for _, entry := range files {
		fileMap, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid torrent: file entry not a dict")
		}
		lengthVal, ok := fileMap["length"].(int64)
		if !ok {
			return nil, fmt.Errorf("invalid torrent: file entry missing length")
		}
		pathVal, ok := fileMap["path"].([]any)
		if !ok {
			return nil, fmt.Errorf("invalid torrent: file entry missing path")
		}
		path := make([]string, len(pathVal))
		for i, p := range pathVal {
			s, ok := p.(string)
			if !ok {
				return nil, fmt.Errorf("invalid torrent: file path component not a string")
			}
			path[i] = s
		}
		entries = append(entries, FileEntry{Length: lengthVal, Path: path})
	}
	return entries, nil
}

func splitPieceHashes(pieces []byte) ([][20]byte, error) {
	if len(pieces)%20 != 0 {
		return nil, fmt.Errorf("invalid pieces length: %d", len(pieces))
	}
	count := len(pieces) / 20
	hashes := make([][20]byte, 0, count)
	for i := range count {
		var hash [20]byte
		copy(hash[:], pieces[i*20:(i+1)*20])
		hashes = append(hashes, hash)
	}
	return hashes, nil
}