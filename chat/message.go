package chat

import (
	"fmt"
	"strings"
	"time"
)

// Container for messages sent to chat
type Message struct {
	Body       string
	from       *User    // Not set for Sys messages
	to         *User    // Only set for PMs
	channel    *Channel // Not set for global commands
	timestamp  time.Time
	themeCache *map[*Theme]string
}

func NewMessage(body string) *Message {
	m := Message{
		Body:      body,
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

// Set channel
func (m *Message) Channel(ch *Channel) *Message {
	m.channel = ch
	return m
}

// Render message based on the given theme
func (m *Message) Render(*Theme) string {
	// TODO: Return []byte?
	// TODO: Render based on theme
	// TODO: Cache based on theme
	var msg string
	if m.to != nil && m.from != nil {
		msg = fmt.Sprintf("[PM from %s] %s", m.from.Name(), m.Body)
	} else if m.from != nil {
		msg = fmt.Sprintf("%s: %s", m.from.Name(), m.Body)
	} else if m.to != nil {
		msg = fmt.Sprintf("-> %s", m.Body)
	} else {
		msg = fmt.Sprintf(" * %s", m.Body)
	}
	return msg
}

// Render message without a theme
func (m *Message) String() string {
	return m.Render(nil)
}

// Wether message is a command (starts with /)
func (m *Message) IsCommand() bool {
	return strings.HasPrefix(m.Body, "/")
}

// Parse command (assumes IsCommand was already called)
func (m *Message) ParseCommand() (string, []string) {
	// TODO: Tokenize this properly, to support quoted args?
	cmd := strings.Split(m.Body, " ")
	args := cmd[1:]
	return cmd[0][1:], args
}
