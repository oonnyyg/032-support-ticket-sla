// Package store provides JSON-file persistence for the desk State.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"example.com/support-ticket-sla/internal/desk"
)

// FileStore keeps all data as one JSON file inside a data directory.
type FileStore struct {
	dir string
}

const stateFileName = "state.json"

// NewFileStore creates a FileStore rooted at dir. The directory is created
// lazily on first save.
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

func (f *FileStore) path() string {
	return filepath.Join(f.dir, stateFileName)
}

// Load reads the state. A missing state file yields an empty, uninitialized
// state rather than an error.
func (f *FileStore) Load() (*desk.State, error) {
	data, err := os.ReadFile(f.path())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &desk.State{Tickets: map[string]*desk.Ticket{}}, nil
		}
		return nil, fmt.Errorf("load state: %w", err)
	}
	var state desk.State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if state.Tickets == nil {
		state.Tickets = map[string]*desk.Ticket{}
	}
	return &state, nil
}

// Save writes the state atomically: a temp file in the same directory is
// fully written and fsynced, then renamed over the real file.
func (f *FileStore) Save(state *desk.State) error {
	if err := os.MkdirAll(f.dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp, err := os.CreateTemp(f.dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp state: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state: %w", err)
	}
	if err := os.Rename(tmpName, f.path()); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}
