package piece

import (
	"fmt"
	"os"
	"path/filepath"

	"go-torrent/internal/torrent"
)

// PieceWriter handles writing completed pieces to the output file(s).
// Supports both single-file and multi-file torrents.
type PieceWriter struct {
	pieceLen int64

	// Single-file mode
	file *os.File

	// Multi-file mode
	isMulti     bool
	entries     []torrent.FileEntry
	fileOffsets []int64 // cumulative byte offset of each file's start
	files       []*os.File
}

// NewPieceWriter creates a PieceWriter for the given torrent.
// For single-file torrents, opens one output file at outputPath/<Name>.
// For multi-file torrents, creates the directory tree at outputPath/<Name>/
// and opens all files.
func NewPieceWriter(outputPath string, tf *torrent.TorrentFile) (*PieceWriter, error) {
	if len(tf.Files) == 0 {
		// Single-file mode
		fullPath := filepath.Join(outputPath, tf.Name)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("creating output directory: %w", err)
		}
		f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return nil, fmt.Errorf("opening output file %s: %w", fullPath, err)
		}
		return &PieceWriter{
			pieceLen: tf.PieceLength,
			file:     f,
		}, nil
	}

	// Multi-file mode
	baseDir := filepath.Join(outputPath, tf.Name)
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("creating output directory: %w", err)
	}

	// Pre-compute cumulative offsets and open all files
	offsets := make([]int64, len(tf.Files))
	files := make([]*os.File, len(tf.Files))
	var cumulative int64
	for i, entry := range tf.Files {
		offsets[i] = cumulative
		cumulative += entry.Length

		fullPath := filepath.Join(append([]string{baseDir}, entry.Path...)...)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			// Close any files already opened
			for j := range files[:i] {
				files[j].Close()
			}
			return nil, fmt.Errorf("creating directory %s: %w", dir, err)
		}
		f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			for j := range files[:i] {
				files[j].Close()
			}
			return nil, fmt.Errorf("opening file %s: %w", fullPath, err)
		}
		files[i] = f
	}

	return &PieceWriter{
		pieceLen:    tf.PieceLength,
		isMulti:     true,
		entries:     tf.Files,
		fileOffsets: offsets,
		files:       files,
	}, nil
}

// WritePiece writes a completed piece's data at its byte offset in the output.
// For single-file torrents: writes to the single output file via WriteAt.
// For multi-file torrents: splits the piece across the correct files.
func (pw *PieceWriter) WritePiece(index uint32, data []byte) error {
	pieceOffset := int64(index) * pw.pieceLen
	pieceEnd := pieceOffset + int64(len(data))

	if pw.file != nil {
		// Single-file: direct WriteAt
		_, err := pw.file.WriteAt(data, pieceOffset)
		return err
	}

	// Multi-file: find overlapping files and write appropriate byte ranges
	for i := range pw.entries {
		fileStart := pw.fileOffsets[i]
		fileEnd := fileStart + pw.entries[i].Length

		overlapStart := max(pieceOffset, fileStart)
		overlapEnd := min(pieceEnd, fileEnd)

		if overlapStart >= overlapEnd {
			continue
		}

		// Offset within the file
		writeAt := overlapStart - fileStart
		// Byte range within `data` to write
		dataStart := overlapStart - pieceOffset
		dataLen := overlapEnd - overlapStart

		if _, err := pw.files[i].WriteAt(data[dataStart:dataStart+dataLen], writeAt); err != nil {
			return fmt.Errorf("writing piece %d to file %d: %w", index, i, err)
		}
	}
	return nil
}

// Close closes all open files.
func (pw *PieceWriter) Close() error {
	if pw.file != nil {
		return pw.file.Close()
	}
	var firstErr error
	for _, f := range pw.files {
		if f != nil {
			if err := f.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}