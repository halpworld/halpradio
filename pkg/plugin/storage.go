package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Storage provides sandboxed key-value persistent storage for an individual plugin.
type Storage struct {
	mu       sync.RWMutex
	dir      string
	filePath string
	data     map[string]string
}

// NewStorage initializes or opens the storage file for a plugin.
func NewStorage(baseDataDir string, pluginID string) (*Storage, error) {
	pluginDir := filepath.Join(baseDataDir, pluginID)
	if err := os.MkdirAll(pluginDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create plugin data dir: %w", err)
	}

	filePath := filepath.Join(pluginDir, "storage.json")
	s := &Storage{
		dir:      pluginDir,
		filePath: filePath,
		data:     make(map[string]string),
	}

	_ = s.load()
	return s, nil
}

func (s *Storage) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	var loaded map[string]string
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}
	s.data = loaded
	return nil
}

func (s *Storage) save() error {
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

// Get retrieves a stored value for a given key.
func (s *Storage) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.data[key]
	return val, ok
}

// Set stores a key-value pair and persists it.
func (s *Storage) Set(key string, val string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = val
	return s.save()
}

// Delete removes a key and persists changes.
func (s *Storage) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return s.save()
}

// GetAll returns a copy of all key-value pairs.
func (s *Storage) GetAll() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copied := make(map[string]string, len(s.data))
	for k, v := range s.data {
		copied[k] = v
	}
	return copied
}

// Dir returns the sandboxed plugin data directory.
func (s *Storage) Dir() string {
	return s.dir
}
