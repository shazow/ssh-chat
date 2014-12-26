package main

import (
	"fmt"
	"io"
	"strings"

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
	ch := chat.NewChannel()
	h := Host{
		listener: listener,
		channel:  ch,
	}
	go ch.Serve()
	return &h
}

// Connect a specific Terminal to this host and its channel
func (h *Host) Connect(term *sshd.Terminal) {
	name := term.Conn.User()
	term.AutoCompleteCallback = h.AutoCompleteFunction

	user := chat.NewUserScreen(name, term)
	go func() {
		// Close term once user is closed.
		user.Wait()
		term.Close()
	}()
	defer user.Close()

	refreshPrompt := func() {
		term.SetPrompt(fmt.Sprintf("[%s] ", user.Name()))
	}
	refreshPrompt()

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
		m := chat.ParseInput(line, user)
		// FIXME: Any reason to use h.channel.Send(m) instead?
		h.channel.HandleMsg(m)
		if m.Command() == "/nick" {
			// Hijack /nick command to update terminal synchronously. Wouldn't
			// work if we use h.channel.Send(m) above.
			// FIXME: This is hacky, how do we improve the API to allow for this?
			refreshPrompt()
		}
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

// AutoCompleteFunction is a callback for terminal autocompletion
func (h *Host) AutoCompleteFunction(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
	if key != 9 {
		return
	}

	fields := strings.Fields(line[:pos])
	partial := fields[len(fields)-1]
	names := h.channel.NamesPrefix(partial)
	if len(names) == 0 {
		// Didn't find anything
		return
	}

	name := names[len(names)-1]
	posPartial := pos - len(partial)

	// Append suffix separator
	if len(fields) < 2 {
		name += ": "
	} else {
		name += " "
	}

	// Reposition the cursor
	newLine = strings.Replace(line[posPartial:], partial, name, 1)
	newLine = line[:posPartial] + newLine
	newPos = pos + (len(name) - len(partial))
	ok = true
	return
}
