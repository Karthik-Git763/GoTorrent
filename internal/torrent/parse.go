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
	Announce    string
	InfoHash    [20]byte
	PieceHashes [][20]byte
	PieceLength int64
	Length      int64
	Name        string
	Files       []FileEntry // non-nil for multi-file torrents; nil for single-file
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

	announce, ok := top["announce"].(string)
	if !ok {
		return fmt.Errorf("invalid torrent: missing announce")
	}
	tf.Announce = announce

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
