package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	MaxStorageKeys  = 1000
	MaxKeyLength    = 256
	MaxValueLength  = 65536       // 64 KB per value
	MaxTotalStorage = 1024 * 1024 // 1 MB total storage per plugin
)

// ValidateStorageKey ensures the key is well-formed, bounded, and contains no control characters.
func ValidateStorageKey(key string) error {
	if key == "" {
		return errors.New("storage key cannot be empty")
	}
	if len(key) > MaxKeyLength {
		return fmt.Errorf("storage key length %d exceeds maximum of %d bytes", len(key), MaxKeyLength)
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c < 0x20 || c == 0x7f {
			return errors.New("storage key contains forbidden control characters")
		}
	}
	return nil
}

// Storage provides sandboxed key-value persistent storage for an individual plugin.
type Storage struct {
	mu       sync.RWMutex
	dir      string
	filePath string
	data     map[string]string
}

// NewStorage initializes or opens the storage file for a plugin.
func NewStorage(baseDataDir string, pluginID string) (*Storage, error) {
	if !validIDRegex.MatchString(pluginID) {
		return nil, fmt.Errorf("invalid plugin ID %q for storage", pluginID)
	}

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
	// Atomic write with restrictive 0600 permissions
	tempFile := s.filePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0600); err != nil {
		return err
	}
	return os.Rename(tempFile, s.filePath)
}

// Get retrieves a stored value for a given key.
func (s *Storage) Get(key string) (string, bool) {
	if err := ValidateStorageKey(key); err != nil {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.data[key]
	return val, ok
}

// Set stores a key-value pair and persists it within quotas.
func (s *Storage) Set(key string, val string) error {
	if err := ValidateStorageKey(key); err != nil {
		return err
	}
	if len(val) > MaxValueLength {
		return fmt.Errorf("storage value length %d exceeds maximum of %d bytes", len(val), MaxValueLength)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.data[key]; !exists && len(s.data) >= MaxStorageKeys {
		return fmt.Errorf("storage key count limit reached (%d keys)", MaxStorageKeys)
	}

	var totalSize int
	for k, v := range s.data {
		if k == key {
			continue
		}
		totalSize += len(k) + len(v)
	}
	totalSize += len(key) + len(val)
	if totalSize > MaxTotalStorage {
		return fmt.Errorf("storage size limit exceeded (%d bytes, max %d bytes)", totalSize, MaxTotalStorage)
	}

	s.data[key] = val
	return s.save()
}

// Delete removes a key and persists changes.
func (s *Storage) Delete(key string) error {
	if err := ValidateStorageKey(key); err != nil {
		return err
	}
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
