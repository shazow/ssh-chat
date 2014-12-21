package chat

import (
	"fmt"
	"time"
)

// Container for messages sent to chat
type Message struct {
	Body       string
	from       *User // Not set for Sys messages
	to         *User // Only set for PMs
	timestamp  time.Time
	themeCache *map[*Theme]string
}

func NewMessage(from *User, body string) *Message {
	m := Message{
		Body:      body,
		from:      from,
		timestamp: time.Now(),
	}
	return &m
}

// Set message recipient
func (m *Message) To(u *User) *Message {
	m.to = u
	return m
}

// Set message sender
func (m *Message) From(u *User) *Message {
	m.from = u
	return m
}

// Render message based on the given theme
func (m *Message) Render(*Theme) string {
	// TODO: Render based on theme.
	// TODO: Cache based on theme
	var msg string
	if m.to != nil {
		msg = fmt.Sprintf("[PM from %s] %s", m.to, m.Body)
	} else if m.from != nil {
		msg = fmt.Sprintf("%s: %s", m.from, m.Body)
	} else {
		msg = fmt.Sprintf(" * %s", m.Body)
	}
	return msg
}

func (m *Message) String() string {
	return m.Render(nil)
}
