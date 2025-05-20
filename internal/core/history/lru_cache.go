package history

import (
	"container/list"
	"fmt"
	"sync"

	"eClip/internal/core/clipboard" // Assuming ClipItem is defined here
)

// CacheItem holds the key and value for an item in the cache list.
// We store the full Item as the value.
type CacheItem struct {
	key   string // Item ID (unique identifier)
	value clipboard.Item
}

// LRUCache implements a thread-safe fixed-size LRU cache for Items.
type LRUCache struct {
	size      int
	evictList *list.List               // Doubly linked list to track usage order (front is most recent)
	items     map[string]*list.Element // Map for quick access to list elements by Item ID
	mu        sync.RWMutex             // Mutex for thread safety
}

// NewLRUCache creates a new LRUCache with the given size.
func NewLRUCache(size int) (*LRUCache, error) {
	if size <= 0 {
		return nil, fmt.Errorf("cache size must be positive")
	}
	return &LRUCache{
		size:      size,
		evictList: list.New(),
		items:     make(map[string]*list.Element),
	}, nil
}

// Add adds an Item to the cache. If the item (identified by its ID) already exists,
// it updates the value and moves the item to the front (most recently used).
// If the cache is full, it evicts the least recently used item before adding the new one.
func (c *LRUCache) Add(item clipboard.Item) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := item.ID // Use Item's ID as the key

	// Check if item already exists
	if element, ok := c.items[key]; ok {
		// Update existing item and move to front
		c.evictList.MoveToFront(element)
		element.Value.(*CacheItem).value = item // Update the value
		return
	}

	// Add new item
	cacheItem := &CacheItem{key: key, value: item}
	element := c.evictList.PushFront(cacheItem)
	c.items[key] = element

	// Check if eviction is needed
	if c.evictList.Len() > c.size {
		c.removeOldest()
	}
}

// Get retrieves an Item from the cache by its ID.
// Returns the item and true if found, otherwise nil and false.
// Moves the accessed item to the front of the list.
func (c *LRUCache) Get(key string) (clipboard.Item, bool) {
	c.mu.Lock() // Use Lock as we might modify the list order
	defer c.mu.Unlock()

	if element, ok := c.items[key]; ok {
		c.evictList.MoveToFront(element)
		// Return a copy to prevent external modification? For now, return direct reference.
		return element.Value.(*CacheItem).value, true
	}
	// Return zero value of Item if not found
	return clipboard.Item{}, false
}

// GetAll retrieves all items currently in the cache, ordered from most to least recently used.
func (c *LRUCache) GetAll() []clipboard.Item {
	c.mu.RLock() // Read lock is sufficient
	defer c.mu.RUnlock()

	items := make([]clipboard.Item, 0, c.evictList.Len())
	for element := c.evictList.Front(); element != nil; element = element.Next() {
		items = append(items, element.Value.(*CacheItem).value)
	}
	return items
}

// Remove removes an item from the cache by its ID.
func (c *LRUCache) Remove(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if element, ok := c.items[key]; ok {
		c.removeElement(element)
		return true
	}
	return false
}

// Len returns the current number of items in the cache.
func (c *LRUCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.evictList.Len()
}

// removeOldest removes the least recently used item from the cache.
// Assumes the write lock is already held.
func (c *LRUCache) removeOldest() {
	element := c.evictList.Back()
	if element != nil {
		c.removeElement(element)
	}
}

// removeElement removes a specific element from the cache.
// Assumes the write lock is already held.
func (c *LRUCache) removeElement(e *list.Element) {
	c.evictList.Remove(e)
	cacheItem := e.Value.(*CacheItem)
	delete(c.items, cacheItem.key)
}
