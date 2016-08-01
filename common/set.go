package common

import (
	"errors"
	"strings"
	"sync"
)

// The error returned when an added id already exists in the set.
var ErrIdTaken = errors.New("id already taken")

// The error returned when a requested item does not exist in the set.
var ErrIdentifiedMissing = errors.New("item does not exist")

// Interface for an item storeable in the set
type Identified interface {
	Id() string
}

// Set with string lookup.
// TODO: Add trie for efficient prefix lookup?
type IdSet struct {
	sync.RWMutex
	lookup map[string]Identified
}

// newIdSet creates a new set.
func NewIdSet() *IdSet {
	return &IdSet{
		lookup: map[string]Identified{},
	}
}

// Clear removes all items and returns the number removed.
func (s *IdSet) Clear() int {
	s.Lock()
	n := len(s.lookup)
	s.lookup = map[string]Identified{}
	s.Unlock()
	return n
}

// Len returns the size of the set right now.
func (s *IdSet) Len() int {
	s.RLock()
	defer s.RUnlock()
	return len(s.lookup)
}

// In checks if an item exists in this set.
func (s *IdSet) In(item Identified) bool {
	s.RLock()
	_, ok := s.lookup[item.Id()]
	s.RUnlock()
	return ok
}

// Get returns an item with the given Id.
func (s *IdSet) Get(id string) (Identified, error) {
	s.RLock()
	item, ok := s.lookup[id]
	s.RUnlock()

	if !ok {
		return nil, ErrIdentifiedMissing
	}

	return item, nil
}

// Add item to this set if it does not exist already.
func (s *IdSet) Add(item Identified) error {
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
func (s *IdSet) Remove(item Identified) error {
	s.Lock()
	defer s.Unlock()
	id := item.Id()
	_, found := s.lookup[id]
	if !found {
		return ErrIdentifiedMissing
	}
	delete(s.lookup, id)
	return nil
}

// Replace item from old id with new Identified.
// Used for moving the same Identified to a new Id, such as a rename.
func (s *IdSet) Replace(oldId string, item Identified) error {
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
		return ErrIdentifiedMissing
	}
	delete(s.lookup, oldId)

	// Add new Identified
	s.lookup[item.Id()] = item

	return nil
}

// Each loops over every item while holding a read lock and applies fn to each
// element.
func (s *IdSet) Each(fn func(item Identified)) {
	s.RLock()
	for _, item := range s.lookup {
		fn(item)
	}
	s.RUnlock()
}

// ListPrefix returns a list of items with a prefix, case insensitive.
func (s *IdSet) ListPrefix(prefix string) []Identified {
	r := []Identified{}
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
