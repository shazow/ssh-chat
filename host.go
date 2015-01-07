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
	commands *chat.Commands

	motd  string
	auth  *Auth
	count int

	// Default theme
	theme *chat.Theme
}

// NewHost creates a Host on top of an existing listener.
func NewHost(listener *sshd.SSHListener) *Host {
	ch := chat.NewChannel()
	h := Host{
		listener: listener,
		channel:  ch,
	}

	// Make our own commands registry instance.
	commands := chat.Commands{}
	chat.InitCommands(&commands)
	ch.SetCommands(commands)

	go ch.Serve()
	return &h
}

// SetMotd sets the host's message of the day.
func (h *Host) SetMotd(motd string) {
	h.motd = motd
}

// Connect a specific Terminal to this host and its channel.
func (h *Host) Connect(term *sshd.Terminal) {
	name := term.Conn.User()
	term.AutoCompleteCallback = h.AutoCompleteFunction

	user := chat.NewUserScreen(name, term)
	user.Config.Theme = h.theme
	go func() {
		// Close term once user is closed.
		user.Wait()
		term.Close()
	}()
	defer user.Close()

	err := h.channel.Join(user)
	if err == chat.ErrIdTaken {
		// Try again...
		user.SetName(fmt.Sprintf("Guest%d", h.count))
		err = h.channel.Join(user)
	}
	if err != nil {
		logger.Errorf("Failed to join: %s", err)
		return
	}

	// Successfully joined.
	term.SetPrompt(GetPrompt(user))
	h.count++

	for {
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

		cmd := m.Command()
		if cmd == "/nick" || cmd == "/theme" {
			// Hijack /nick command to update terminal synchronously. Wouldn't
			// work if we use h.channel.Send(m) above.
			//
			// FIXME: This is hacky, how do we improve the API to allow for
			// this? Chat module shouldn't know about terminals.
			term.SetPrompt(GetPrompt(user))
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

// GetPrompt will render the terminal prompt string based on the user.
func GetPrompt(user *chat.User) string {
	name := user.Name()
	if user.Config.Theme != nil {
		name = user.Config.Theme.ColorName(user)
	}
	return fmt.Sprintf("[%s] ", name)
}
