package sshchat

import (
	"sync"
	"time"
)

type expiringValue struct {
	time.Time
}

func (v expiringValue) Bool() bool {
	return time.Now().Before(v.Time)
}

type value struct{}

func (v value) Bool() bool {
	return true
}

type setValue interface {
	Bool() bool
}

// Set with expire-able keys
type Set struct {
	sync.Mutex
	lookup map[string]setValue
}

// NewSet creates a new set.
func NewSet() *Set {
	return &Set{
		lookup: map[string]setValue{},
	}
}

// Len returns the size of the set right now.
func (s *Set) Len() int {
	s.Lock()
	defer s.Unlock()
	return len(s.lookup)
}

// In checks if an item exists in this set.
func (s *Set) In(key string) bool {
	s.Lock()
	v, ok := s.lookup[key]
	if ok && !v.Bool() {
		ok = false
		delete(s.lookup, key)
	}
	s.Unlock()
	return ok
}

// Add item to this set, replace if it exists.
func (s *Set) Add(key string) {
	s.Lock()
	s.lookup[key] = value{}
	s.Unlock()
}

// Add item to this set, replace if it exists.
func (s *Set) AddExpiring(key string, d time.Duration) time.Time {
	until := time.Now().Add(d)
	s.Lock()
	s.lookup[key] = expiringValue{until}
	s.Unlock()
	return until
}
