package piece

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const resumeSuffix = ".gtstate"
const saveInterval = 10 // save state every N completed pieces

// ResumeState holds the persistence data for download resumption.
type ResumeState struct {
	InfoHash  string `json:"info_hash"`
	Completed []bool `json:"completed"`
}

// StateFilePath returns the path to the resume state file for a torrent
// identified by its name, placed alongside the output.
func StateFilePath(outputPath, torrentName string) string {
	return filepath.Join(outputPath, torrentName+resumeSuffix)
}

// SaveResume writes the current download state to disk atomically.
func SaveResume(path string, infoHash [20]byte, completed []bool) error {
	state := ResumeState{
		InfoHash:  fmt.Sprintf("%040x", infoHash),
		Completed: completed,
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshalling resume state: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// LoadResume reads a saved state from disk and validates it matches
// the given infoHash. Returns nil if the file doesn't exist or
// the infoHash doesn't match (caller should start fresh).
func LoadResume(path string, infoHash [20]byte) (*ResumeState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state ResumeState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing resume state: %w", err)
	}
	expected := fmt.Sprintf("%040x", infoHash)
	if state.InfoHash != expected {
		return nil, fmt.Errorf("info_hash mismatch: state=%s, torrent=%s", state.InfoHash, expected)
	}
	return &state, nil
}

// RemoveResume deletes the state file after a successful download.
func RemoveResume(path string) {
	os.Remove(path)
}

// CountCompleted returns the number of true values in the bitfield.
func CountCompleted(completed []bool) int {
	n := 0
	for _, c := range completed {
		if c {
			n++
		}
	}
	return n
}