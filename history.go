// TODO: Split this out into its own module, it's kinda neat.
package main

type History struct {
	entries []string
	head    int
	size    int
}

func NewHistory(size int) *History {
	return &History{
		entries: make([]string, size),
	}
}

func (h *History) Add(entry string) {
	max := cap(h.entries)
	h.head = (h.head + 1) % max
	h.entries[h.head] = entry
	if h.size < max {
		h.size++
	}
}

func (h *History) Len() int {
	return h.size
}

func (h *History) Get(num int) []string {
	max := cap(h.entries)
	if num > h.size {
		num = h.size
	}

	r := make([]string, num)
	for i := 0; i < num; i++ {
		idx := (h.head - i) % max
		if idx < 0 {
			idx += max
		}
		r[num-i-1] = h.entries[idx]
	}

	return r
}
