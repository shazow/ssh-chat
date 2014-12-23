package chat

import (
	"errors"
	"strings"
	"sync"
)

var ErrIdTaken error = errors.New("id already taken")
var ErrItemMissing error = errors.New("item does not exist")

// Unique identifier for an item
type Id string

// A prefix for a unique identifier
type IdPrefix Id

// An interface for items to store-able in the set
type Item interface {
	Id() Id
}

// Set with string lookup
// TODO: Add trie for efficient prefix lookup?
type Set struct {
	lookup map[Id]Item
	sync.RWMutex
}

// Create a new set
func NewSet() *Set {
	return &Set{
		lookup: map[Id]Item{},
	}
}

// Remove all items and return the number removed
func (s *Set) Clear() int {
	s.Lock()
	n := len(s.lookup)
	s.lookup = map[Id]Item{}
	s.Unlock()
	return n
}

// Size of the set right now
func (s *Set) Len() int {
	return len(s.lookup)
}

// Check if user belongs in this set
func (s *Set) In(item Item) bool {
	s.RLock()
	_, ok := s.lookup[item.Id()]
	s.RUnlock()
	return ok
}

// Get user by name
func (s *Set) Get(id Id) (Item, error) {
	s.RLock()
	item, ok := s.lookup[id]
	s.RUnlock()

	if !ok {
		return nil, ErrItemMissing
	}

	return item, nil
}

// Add user to set if user does not exist already
func (s *Set) Add(item Item) error {
	s.Lock()
	defer s.Unlock()

	_, found := s.lookup[item.Id()]
	if found {
		return ErrIdTaken
	}

	s.lookup[item.Id()] = item
	return nil
}

// Remove user from set
func (s *Set) Remove(item Item) error {
	s.Lock()
	defer s.Unlock()
	id := item.Id()
	_, found := s.lookup[id]
	if found {
		return ErrItemMissing
	}
	delete(s.lookup, id)
	return nil
}

// Loop over every item while holding a read lock and apply fn
func (s *Set) Each(fn func(item Item)) {
	s.RLock()
	for _, item := range s.lookup {
		fn(item)
	}
	s.RUnlock()
}

// List users by prefix, case insensitive
func (s *Set) ListPrefix(prefix string) []Item {
	r := []Item{}
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
