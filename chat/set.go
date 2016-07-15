package chat

import (
	"errors"
	"strings"
	"sync"
)

// The error returned when an added id already exists in the set.
var ErrIdTaken = errors.New("id already taken")

// The error returned when a requested item does not exist in the set.
var ErridentifiedMissing = errors.New("item does not exist")

// Interface for an item storeable in the set
type identified interface {
	Id() string
}

// Set with string lookup.
// TODO: Add trie for efficient prefix lookup?
type idSet struct {
	sync.RWMutex
	lookup map[string]identified
}

// newIdSet creates a new set.
func newIdSet() *idSet {
	return &idSet{
		lookup: map[string]identified{},
	}
}

// Clear removes all items and returns the number removed.
func (s *idSet) Clear() int {
	s.Lock()
	n := len(s.lookup)
	s.lookup = map[string]identified{}
	s.Unlock()
	return n
}

// Len returns the size of the set right now.
func (s *idSet) Len() int {
	s.RLock()
	defer s.RUnlock()
	return len(s.lookup)
}

// In checks if an item exists in this set.
func (s *idSet) In(item identified) bool {
	s.RLock()
	_, ok := s.lookup[item.Id()]
	s.RUnlock()
	return ok
}

// Get returns an item with the given Id.
func (s *idSet) Get(id string) (identified, error) {
	s.RLock()
	item, ok := s.lookup[id]
	s.RUnlock()

	if !ok {
		return nil, ErridentifiedMissing
	}

	return item, nil
}

// Add item to this set if it does not exist already.
func (s *idSet) Add(item identified) error {
	s.Lock()
	defer s.Unlock()

	_, found := s.lookup[item.Id()]
	if found {
		return ErrIdTaken
	}

	s.lookup[item.Id()] = item
	return nil
}

// Remove item from this set.
func (s *idSet) Remove(item identified) error {
	s.Lock()
	defer s.Unlock()
	id := item.Id()
	_, found := s.lookup[id]
	if !found {
		return ErridentifiedMissing
	}
	delete(s.lookup, id)
	return nil
}

// Replace item from old id with new identified.
// Used for moving the same identified to a new Id, such as a rename.
func (s *idSet) Replace(oldId string, item identified) error {
	s.Lock()
	defer s.Unlock()

	// Check if it already exists
	_, found := s.lookup[item.Id()]
	if found {
		return ErrIdTaken
	}

	// Remove oldId
	_, found = s.lookup[oldId]
	if !found {
		return ErridentifiedMissing
	}
	delete(s.lookup, oldId)

	// Add new identified
	s.lookup[item.Id()] = item

	return nil
}

// Each loops over every item while holding a read lock and applies fn to each
// element.
func (s *idSet) Each(fn func(item identified)) {
	s.RLock()
	for _, item := range s.lookup {
		fn(item)
	}
	s.RUnlock()
}

// ListPrefix returns a list of items with a prefix, case insensitive.
func (s *idSet) ListPrefix(prefix string) []identified {
	r := []identified{}
	prefix = strings.ToLower(prefix)

	s.RLock()
	defer s.RUnlock()

	for id, item := range s.lookup {
		if !strings.HasPrefix(string(id), prefix) {
			continue
		}
		r = append(r, item)
	}

	return r
}
