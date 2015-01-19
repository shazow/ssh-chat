package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/sshd"
)

// GetPrompt will render the terminal prompt string based on the user.
func GetPrompt(user *chat.User) string {
	name := user.Name()
	if user.Config.Theme != nil {
		name = user.Config.Theme.ColorName(user)
	}
	return fmt.Sprintf("[%s] ", name)
}

// Host is the bridge between sshd and chat modules
// TODO: Should be easy to add support for multiple rooms, if we want.
type Host struct {
	*chat.Room
	listener *sshd.SSHListener
	commands chat.Commands

	motd  string
	auth  *Auth
	count int

	// Default theme
	theme *chat.Theme
}

// NewHost creates a Host on top of an existing listener.
func NewHost(listener *sshd.SSHListener) *Host {
	room := chat.NewRoom()
	h := Host{
		Room:     room,
		listener: listener,
		commands: chat.Commands{},
	}

	// Make our own commands registry instance.
	chat.InitCommands(&h.commands)
	h.InitCommands(&h.commands)
	room.SetCommands(h.commands)

	go room.Serve()
	return &h
}

// SetMotd sets the host's message of the day.
func (h *Host) SetMotd(motd string) {
	h.motd = motd
}

func (h Host) isOp(conn sshd.Connection) bool {
	key := conn.PublicKey()
	if key == nil {
		return false
	}
	return h.auth.IsOp(key)
}

// Connect a specific Terminal to this host and its room.
func (h *Host) Connect(term *sshd.Terminal) {
	id := NewIdentity(term.Conn)
	user := chat.NewUserScreen(id, term)
	user.Config.Theme = h.theme
	go func() {
		// Close term once user is closed.
		user.Wait()
		term.Close()
	}()
	defer user.Close()

	member, err := h.Join(user)
	if err == chat.ErrIdTaken {
		// Try again...
		id.SetName(fmt.Sprintf("Guest%d", h.count))
		member, err = h.Join(user)
	}
	if err != nil {
		logger.Errorf("Failed to join: %s", err)
		return
	}

	// Successfully joined.
	term.SetPrompt(GetPrompt(user))
	term.AutoCompleteCallback = h.AutoCompleteFunction(user)
	user.SetHighlight(user.Name())
	h.count++

	// Should the user be op'd on join?
	member.Op = h.isOp(term.Conn)

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

		// FIXME: Any reason to use h.room.Send(m) instead?
		h.HandleMsg(m)

		cmd := m.Command()
		if cmd == "/nick" || cmd == "/theme" {
			// Hijack /nick command to update terminal synchronously. Wouldn't
			// work if we use h.room.Send(m) above.
			//
			// FIXME: This is hacky, how do we improve the API to allow for
			// this? Chat module shouldn't know about terminals.
			term.SetPrompt(GetPrompt(user))
			user.SetHighlight(user.Name())
		}
	}

	err = h.Leave(user)
	if err != nil {
		logger.Errorf("Failed to leave: %s", err)
		return
	}
}

// Serve our chat room onto the listener
func (h *Host) Serve() {
	terminals := h.listener.ServeTerminal()

	for term := range terminals {
		go h.Connect(term)
	}
}

func (h Host) completeName(partial string) string {
	names := h.NamesPrefix(partial)
	if len(names) == 0 {
		// Didn't find anything
		return ""
	}

	return names[len(names)-1]
}

func (h Host) completeCommand(partial string) string {
	for cmd, _ := range h.commands {
		if strings.HasPrefix(cmd, partial) {
			return cmd
		}
	}
	return ""
}

// AutoCompleteFunction returns a callback for terminal autocompletion
func (h *Host) AutoCompleteFunction(u *chat.User) func(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
	return func(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
		if key != 9 {
			return
		}

		if strings.HasSuffix(line[:pos], " ") {
			// Don't autocomplete spaces.
			return
		}

		fields := strings.Fields(line[:pos])
		isFirst := len(fields) < 2
		partial := fields[len(fields)-1]
		posPartial := pos - len(partial)

		var completed string
		if isFirst && strings.HasPrefix(partial, "/") {
			// Command
			completed = h.completeCommand(partial)
			if completed == "/reply" {
				replyTo := u.ReplyTo()
				if replyTo != nil {
					completed = "/msg " + replyTo.Name()
				}
			}
		} else {
			// Name
			completed = h.completeName(partial)
			if completed == "" {
				return
			}
			if isFirst {
				completed += ":"
			}
		}
		completed += " "

		// Reposition the cursor
		newLine = strings.Replace(line[posPartial:], partial, completed, 1)
		newLine = line[:posPartial] + newLine
		newPos = pos + (len(completed) - len(partial))
		ok = true
		return
	}
}

// GetUser returns a chat.User based on a name.
func (h *Host) GetUser(name string) (*chat.User, bool) {
	m, ok := h.MemberById(name)
	if !ok {
		return nil, false
	}
	return m.User, true
}

// InitCommands adds host-specific commands to a Commands container. These will
// override any existing commands.
func (h *Host) InitCommands(c *chat.Commands) {
	c.Add(chat.Command{
		Prefix:     "/msg",
		PrefixHelp: "USER MESSAGE",
		Help:       "Send MESSAGE to USER.",
		Handler: func(room *chat.Room, msg chat.CommandMsg) error {
			args := msg.Args()
			switch len(args) {
			case 0:
				return errors.New("must specify user")
			case 1:
				return errors.New("must specify message")
			}

			target, ok := h.GetUser(args[0])
			if !ok {
				return errors.New("user not found")
			}

			m := chat.NewPrivateMsg(strings.Join(args[1:], " "), msg.From(), target)
			room.Send(m)
			return nil
		},
	})

	c.Add(chat.Command{
		Prefix:     "/reply",
		PrefixHelp: "MESSAGE",
		Help:       "Reply with MESSAGE to the previous private message.",
		Handler: func(room *chat.Room, msg chat.CommandMsg) error {
			args := msg.Args()
			switch len(args) {
			case 0:
				return errors.New("must specify message")
			}

			target := msg.From().ReplyTo()
			if target == nil {
				return errors.New("no message to reply to")
			}

			m := chat.NewPrivateMsg(strings.Join(args, " "), msg.From(), target)
			room.Send(m)
			return nil
		},
	})

	// Op commands
	c.Add(chat.Command{
		Op:         true,
		Prefix:     "/kick",
		PrefixHelp: "USER",
		Help:       "Kick USER from the server.",
		Handler: func(room *chat.Room, msg chat.CommandMsg) error {
			if !room.IsOp(msg.From()) {
				return errors.New("must be op")
			}

			args := msg.Args()
			if len(args) == 0 {
				return errors.New("must specify user")
			}

			target, ok := h.GetUser(args[0])
			if !ok {
				return errors.New("user not found")
			}

			body := fmt.Sprintf("%s was kicked by %s.", target.Name(), msg.From().Name())
			room.Send(chat.NewAnnounceMsg(body))
			target.Close()
			return nil
		},
	})

	c.Add(chat.Command{
		Op:         true,
		Prefix:     "/ban",
		PrefixHelp: "USER",
		Help:       "Ban USER from the server.",
		Handler: func(room *chat.Room, msg chat.CommandMsg) error {
			// TODO: Would be nice to specify what to ban. Key? Ip? etc.
			if !room.IsOp(msg.From()) {
				return errors.New("must be op")
			}

			args := msg.Args()
			if len(args) == 0 {
				return errors.New("must specify user")
			}

			target, ok := h.GetUser(args[0])
			if !ok {
				return errors.New("user not found")
			}

			id := target.Identifier.(*Identity)
			h.auth.Ban(id.PublicKey())
			h.auth.BanAddr(id.RemoteAddr())

			body := fmt.Sprintf("%s was banned by %s.", target.Name(), msg.From().Name())
			room.Send(chat.NewAnnounceMsg(body))
			target.Close()

			logger.Debugf("Banned: \n-> %s", id.Whois())

			return nil
		},
	})

	c.Add(chat.Command{
		Op:         true,
		Prefix:     "/whois",
		PrefixHelp: "USER",
		Help:       "Information about USER.",
		Handler: func(room *chat.Room, msg chat.CommandMsg) error {
			// TODO: Would be nice to specify what to ban. Key? Ip? etc.
			if !room.IsOp(msg.From()) {
				return errors.New("must be op")
			}

			args := msg.Args()
			if len(args) == 0 {
				return errors.New("must specify user")
			}

			target, ok := h.GetUser(args[0])
			if !ok {
				return errors.New("user not found")
			}

			id := target.Identifier.(*Identity)
			room.Send(chat.NewSystemMsg(id.Whois(), msg.From()))

			return nil
		},
	})
}
