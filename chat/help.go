package chat

import (
	"fmt"
	"sort"
	"strings"
)

type helpItem struct {
	Prefix string
	Text   string
}

type help struct {
	items       []helpItem
	prefixWidth int
}

// NewCommandsHelp creates a help container from a commands container.
func NewCommandsHelp(c *Commands) *help {
	lookup := map[string]struct{}{}
	h := help{
		items: []helpItem{},
	}
	for _, cmd := range c.commands {
		if cmd.Help == "" {
			// Skip hidden commands.
			continue
		}
		_, exists := lookup[cmd.Prefix]
		if exists {
			// Duplicate (alias)
			continue
		}
		lookup[cmd.Prefix] = struct{}{}
		prefix := fmt.Sprintf("%s %s", cmd.Prefix, cmd.PrefixHelp)
		h.add(helpItem{prefix, cmd.Help})
	}
	return &h
}

func (h *help) add(item helpItem) {
	h.items = append(h.items, item)
	if len(item.Prefix) > h.prefixWidth {
		h.prefixWidth = len(item.Prefix)
	}
}

func (h help) String() string {
	r := []string{}
	format := fmt.Sprintf("%%-%ds - %%s", h.prefixWidth)
	for _, item := range h.items {
		r = append(r, fmt.Sprintf(format, item.Prefix, item.Text))
	}

	sort.Strings(r)
	return strings.Join(r, Newline)
}
