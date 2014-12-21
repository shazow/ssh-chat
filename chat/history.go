package chat

import "sync"

// History contains the history entries
type History struct {
	entries []interface{}
	head    int
	size    int
	sync.RWMutex
}

// NewHistory constructs a new history of the given size
func NewHistory(size int) *History {
	return &History{
		entries: make([]interface{}, size),
	}
}

// Add adds the given entry to the entries in the history
func (h *History) Add(entry interface{}) {
	h.Lock()
	defer h.Unlock()

	max := cap(h.entries)
	h.head = (h.head + 1) % max
	h.entries[h.head] = entry
	if h.size < max {
		h.size++
	}
}

// Len returns the number of entries in the history
func (h *History) Len() int {
	return h.size
}

// Get recent entries
func (h *History) Get(num int) []interface{} {
	h.RLock()
	defer h.RUnlock()

	max := cap(h.entries)
	if num > h.size {
		num = h.size
	}

	r := make([]interface{}, num)
	for i := 0; i < num; i++ {
		idx := (h.head - i) % max
		if idx < 0 {
			idx += max
		}
		r[num-i-1] = h.entries[idx]
	}

	return r
}
