package main

import (
	"fmt"
	"io"

	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/sshd"
)

// Host is the bridge between sshd and chat modules
// TODO: Should be easy to add support for multiple channels, if we want.
type Host struct {
	listener *sshd.SSHListener
	channel  *chat.Channel
}

// NewHost creates a Host on top of an existing listener
func NewHost(listener *sshd.SSHListener) *Host {
	h := Host{
		listener: listener,
		channel:  chat.NewChannel(),
	}
	return &h
}

// Connect a specific Terminal to this host and its channel
func (h *Host) Connect(term *sshd.Terminal) {
	defer term.Close()
	name := term.Conn.User()
	term.SetPrompt(fmt.Sprintf("[%s] ", name))
	// TODO: term.AutoCompleteCallback = ...

	user := chat.NewUserScreen(name, term)
	defer user.Close()

	err := h.channel.Join(user)
	if err != nil {
		logger.Errorf("Failed to join: %s", err)
		return
	}

	for {
		// TODO: Handle commands etc?
		line, err := term.ReadLine()
		if err == io.EOF {
			// Closed
			break
		} else if err != nil {
			logger.Errorf("Terminal reading error: %s", err)
			break
		}
		m := chat.NewMessage(line).From(user)
		h.channel.Send(*m)
	}

	err = h.channel.Leave(user)
	if err != nil {
		logger.Errorf("Failed to leave: %s", err)
		return
	}
}

// Serve our chat channel onto the listener
func (h *Host) Serve() {
	terminals := h.listener.ServeTerminal()

	for term := range terminals {
		go h.Connect(term)
	}
}

/* TODO: ...
func (h *Host) AutoCompleteFunction(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
	if key != 9 {
		return
	}

	shortLine := strings.Split(line[:pos], " ")
	partialNick := shortLine[len(shortLine)-1]
	nicks := h.channel.users.ListPrefix(&partialNick)
	if len(nicks) == 0 {
		return
	}

	nick := nicks[len(nicks)-1]
	posPartialNick := pos - len(partialNick)
	if len(shortLine) < 2 {
		nick += ": "
	} else {
		nick += " "
	}
	newLine = strings.Replace(line[posPartialNick:], partialNick, nick, 1)
	newLine = line[:posPartialNick] + newLine
	newPos = pos + (len(nick) - len(partialNick))
	ok = true
	return
}
*/
