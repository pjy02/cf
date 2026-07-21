package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/pjy02/cf/internal/model"
)

type CacheEntry struct {
	SourceTime string       `json:"source_time,omitempty"`
	FetchedAt  time.Time    `json:"fetched_at"`
	Nodes      []model.Node `json:"nodes"`
}

type State struct {
	Version      int                   `json:"version"`
	UpdatedAt    time.Time             `json:"updated_at"`
	LastSuccess  time.Time             `json:"last_success,omitempty"`
	LastError    string                `json:"last_error,omitempty"`
	LastStatuses map[string]string     `json:"last_statuses,omitempty"`
	Carriers     map[string]CacheEntry `json:"carriers"`
}

func Empty() State {
	return State{Version: 1, Carriers: make(map[string]CacheEntry), LastStatuses: make(map[string]string)}
}

func Load(path string) (State, error) {
	s := Empty()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return Empty(), err
	}
	if s.Carriers == nil {
		s.Carriers = make(map[string]CacheEntry)
	}
	if s.LastStatuses == nil {
		s.LastStatuses = make(map[string]string)
	}
	return s, nil
}

func Save(path string, s State) error {
	s.Version = 1
	s.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0o600)
}
