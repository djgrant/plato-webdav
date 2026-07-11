package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const stateFileName = ".sync-state.json"

// FileState records what we last downloaded for one file, keyed by the
// sanitized path relative to the save directory.
type FileState struct {
	Href     string `json:"href"` // decoded server path, for matching remote listings
	ETag     string `json:"etag,omitempty"`
	Modified string `json:"modified,omitempty"`
	Size     int64  `json:"size"`
}

type State struct {
	Files map[string]FileState `json:"files"`
}

func loadState(saveDir string) *State {
	st := &State{Files: map[string]FileState{}}
	data, err := os.ReadFile(filepath.Join(saveDir, stateFileName))
	if err != nil {
		return st
	}
	if json.Unmarshal(data, st) != nil || st.Files == nil {
		st.Files = map[string]FileState{}
	}
	return st
}

// save writes the state atomically (temp file + rename).
func (st *State) save(saveDir string) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(saveDir, stateFileName+".tmp")
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(saveDir, stateFileName))
}
