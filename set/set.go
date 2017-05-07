package set

import (
	"errors"
	"strings"
	"sync"
)

// Returned when an added key already exists in the set.
var ErrCollision = errors.New("key already exists")

// Returned when a requested item does not exist in the set.
var ErrMissing = errors.New("item does not exist")

// Returned when a nil item is added. Nil values are considered expired and invalid.
var ErrNil = errors.New("item value must not be nil")

type IterFunc func(key string, item Item) error

type Set struct {
	sync.RWMutex
	lookup    map[string]Item
	normalize func(string) string
}

// New creates a new set with case-insensitive keys
func New() *Set {
	return &Set{
		lookup:    map[string]Item{},
		normalize: normalize,
	}
}

// Clear removes all items and returns the number removed.
func (s *Set) Clear() int {
	s.Lock()
	n := len(s.lookup)
	s.lookup = map[string]Item{}
	s.Unlock()
	return n
}

// Len returns the size of the set right now.
func (s *Set) Len() int {
	s.RLock()
	defer s.RUnlock()
	return len(s.lookup)
}

// In checks if an item exists in this set.
func (s *Set) In(key string) bool {
	key = s.normalize(key)
	s.RLock()
	item, ok := s.lookup[key]
	s.RUnlock()
	if ok && item.Value() == nil {
		s.cleanup(key)
		ok = false
	}
	return ok
}

// Get returns an item with the given key.
func (s *Set) Get(key string) (Item, error) {
	key = s.normalize(key)
	s.RLock()
	item, ok := s.lookup[key]
	s.RUnlock()

	if ok && item.Value() == nil {
		s.cleanup(key)
		ok = false
	}
	if !ok {
		return nil, ErrMissing
	}

	return item, nil
}

// Remove potentially expired key (normalized).
func (s *Set) cleanup(key string) {
	s.Lock()
	item, ok := s.lookup[key]
	if ok && item == nil {
		delete(s.lookup, key)
	}
	s.Unlock()
}

// Add item to this set if it does not exist already.
func (s *Set) AddNew(item Item) error {
	key := s.normalize(item.Key())

	s.Lock()
	defer s.Unlock()

	oldItem, found := s.lookup[key]
	if found && oldItem.Value() != nil {
		return ErrCollision
	}

	if item.Value() == nil {
		delete(s.lookup, key)
	} else {
		s.lookup[key] = item
	}
	return nil
}

// Add to set, replacing if item already exists.
func (s *Set) Add(item Item) error {
	key := s.normalize(item.Key())

	s.Lock()
	defer s.Unlock()

	if item.Value() == nil {
		delete(s.lookup, key)
	} else {
		s.lookup[key] = item
	}
	return nil
}

// Remove item from this set.
func (s *Set) Remove(key string) error {
	key = s.normalize(key)

	s.Lock()
	defer s.Unlock()

	_, found := s.lookup[key]
	if !found {
		return ErrMissing
	}
	delete(s.lookup, key)
	return nil
}

// Replace oldKey with a new item, which might be a new key.
// Can be used to rename items.
func (s *Set) Replace(oldKey string, item Item) error {
	newKey := s.normalize(item.Key())
	oldKey = s.normalize(oldKey)

	s.Lock()
	defer s.Unlock()

	if newKey != oldKey {
		// Check if it already exists
		_, found := s.lookup[newKey]
		if found {
			return ErrCollision
		}

		// Remove oldKey
		delete(s.lookup, oldKey)
	}

	if item.Value() == nil {
		delete(s.lookup, newKey)
	} else {
		// Add new item
		s.lookup[newKey] = item
	}

	return nil
}

// Each loops over every item while holding a read lock and applies fn to each
// element.
func (s *Set) Each(fn IterFunc) error {
	s.RLock()
	defer s.RUnlock()
	for key, item := range s.lookup {
		if item.Value() == nil {
			defer s.cleanup(key)
			continue
		}
		if err := fn(key, item); err != nil {
			// Abort early
			return err
		}
	}
	return nil
}

// ListPrefix returns a list of items with a prefix, normalized.
// TODO: Add trie for efficient prefix lookup
func (s *Set) ListPrefix(prefix string) []Item {
	r := []Item{}
	prefix = s.normalize(prefix)

	s.Each(func(key string, item Item) error {
		if strings.HasPrefix(key, prefix) {
			r = append(r, item)
		}
		return nil
	})

	return r
}

func normalize(key string) string {
	return strings.ToLower(key)
}
