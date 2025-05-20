package history

import (
	"fmt"
	"sync"

	"eClip/internal/core/clipboard" // Assuming ClipItem is defined here
	"eClip/internal/logger"         // For logging
)

const (
	DefaultHistoryCapacity = 100 // Default number of items to keep in history
)

// Storage manages the clipboard history.
// It uses an LRU cache for efficient in-memory storage.
type Storage struct {
	cache    *LRUCache
	capacity int
	mu       sync.RWMutex // To protect access to storage-level operations if any (e.g., reconfiguring capacity)
	logger   *logger.AsyncLogger
}

// NewStorage creates a new history Storage instance.
// It initializes an LRU cache with the given capacity.
func NewStorage(capacity int, log *logger.AsyncLogger) (*Storage, error) {
	if capacity <= 0 {
		capacity = DefaultHistoryCapacity
		log.Infof("History capacity not specified or invalid, using default: %d", capacity)
	}
	lru, err := NewLRUCache(capacity)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache for history: %w", err)
	}
	return &Storage{
		cache:    lru,
		capacity: capacity,
		logger:   log,
	}, nil
}

// AddItem adds a new clipboard item to the history.
func (s *Storage) AddItem(item clipboard.Item) {
	if item.ID == "" {
		s.logger.Warnf("Attempted to add Item with empty ID. Item: %+v", item)
		// Potentially generate an ID here if it's missing, or reject.
		// For now, we rely on the caller to provide a valid ID.
		// If IDs are generated based on content hash, this check is crucial.
		return
	}
	s.cache.Add(item)
	s.logger.Debugf("Added item to history: %s (Type: %d)", item.ID, item.Type)
}

// GetItem retrieves a specific item from history by its ID.
func (s *Storage) GetItem(id string) (clipboard.Item, bool) {
	item, found := s.cache.Get(id)
	if found {
		s.logger.Debugf("Retrieved item from history: %s", id)
	} else {
		s.logger.Debugf("Item not found in history: %s", id)
	}
	return item, found
}

// GetAllItems retrieves all items currently in the history,
// ordered from most to least recently used.
func (s *Storage) GetAllItems() []clipboard.Item {
	items := s.cache.GetAll()
	s.logger.Debugf("Retrieved all %d items from history", len(items))
	return items
}

// ClearHistory removes all items from the history.
func (s *Storage) ClearHistory() {
	s.mu.Lock() // Lock for potential re-creation or full clear operations
	defer s.mu.Unlock()

	// Re-create the cache to clear it effectively
	lru, err := NewLRUCache(s.capacity)
	if err != nil {
		// This should ideally not happen if capacity is valid
		s.logger.Errorf("Failed to re-create LRU cache during clear: %v", err)
		// Fallback: try to remove items one by one (less efficient)
		// Or, if LRUCache had a Clear() method, use that.
		// For now, we assume NewLRUCache will succeed.
		// If not, the old cache remains, which is not ideal for a "Clear" operation.
		// A robust implementation might need a Clear method in LRUCache itself.
		// For simplicity, we'll reassign.
		currentItems := s.cache.GetAll() // Get all keys to remove
		for _, item := range currentItems {
			s.cache.Remove(item.ID)
		}

	} else {
		s.cache = lru
	}
	s.logger.Infof("Clipboard history cleared.")
}

// GetCapacity returns the current capacity of the history storage.
func (s *Storage) GetCapacity() int {
	return s.capacity
}

// GetCurrentSize returns the current number of items in the history.
func (s *Storage) GetCurrentSize() int {
	return s.cache.Len()
}

// TODO: Implement search functionality if needed.
// SearchItems(query string, filter ClipType) ([]ClipItem, error)

// TODO: Implement persistence (loading/saving history to disk).
// Load() error
// Save() error
