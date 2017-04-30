package sshchat

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/shazow/rateio"
	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/set"
	"github.com/shazow/ssh-chat/sshd"
)

const maxInputLength int = 1024

// Host is the bridge between sshd and chat modules
// TODO: Should be easy to add support for multiple rooms, if we want.
type Host struct {
	*chat.Room
	listener *sshd.SSHListener
	commands chat.Commands
	auth     *Auth

	// Version string to print on /version
	Version string

	// Default theme
	theme message.Theme

	mu    sync.Mutex
	motd  string
	count int
}

// NewHost creates a Host on top of an existing listener.
func NewHost(listener *sshd.SSHListener, auth *Auth) *Host {
	room := chat.NewRoom()
	h := Host{
		Room:     room,
		listener: listener,
		commands: chat.Commands{},
		auth:     auth,
	}

	// Make our own commands registry instance.
	chat.InitCommands(&h.commands)
	h.InitCommands(&h.commands)
	room.SetCommands(h.commands)

	go room.Serve()
	return &h
}

// SetTheme sets the default theme for the host.
func (h *Host) SetTheme(theme message.Theme) {
	h.mu.Lock()
	h.theme = theme
	h.mu.Unlock()
}

// SetMotd sets the host's message of the day.
func (h *Host) SetMotd(motd string) {
	h.mu.Lock()
	h.motd = motd
	h.mu.Unlock()
}

var globalUser *client

// Connect a specific Terminal to this host and its room.
func (h *Host) Connect(t *sshd.Terminal) {
	// XXX: Hack to test multiple users per key
	if globalUser != nil {
		globalUser.Add(t)
		return
	}

	conn := t.Conn
	remoteAddr := conn.RemoteAddr()
	requestedName := conn.Name()
	term := sshd.MultiTerm(t)
	screen := message.BufferedScreen(requestedName, term)

	user := &client{
		Member:    screen,
		multiTerm: term,
	}
	defer user.Close()
	// XXX: Hack to test multiple users per key
	globalUser = user

	h.mu.Lock()
	motd := h.motd
	count := h.count
	h.count++
	h.mu.Unlock()

	cfg := user.Config()
	cfg.Theme = &h.theme
	user.SetConfig(cfg)

	go screen.Consume()

	// Send MOTD
	if motd != "" {
		user.Send(message.NewAnnounceMsg(motd))
	}

	member, err := h.Join(user)
	if err != nil {
		// Try again...
		user.SetName(fmt.Sprintf("Guest%d", count))
		member, err = h.Join(user)
	}
	if err != nil {
		logger.Errorf("[%s] Failed to join: %s", conn.RemoteAddr(), err)
		return
	}

	// Successfully joined.
	term.SetPrompt(user.Prompt())
	term.AutoCompleteCallback = h.AutoCompleteFunction(user)
	user.SetHighlight(user.Name())

	// XXX: Mark multiterm as ready?

	// Should the user be op'd on join?
	if key := conn.PublicKey(); key != nil {
		authItem, err := h.auth.ops.Get(newAuthKey(key))
		if err == nil {
			err = h.Room.Ops.Add(set.Rename(authItem, member.ID()))
		}
	}
	if err != nil {
		logger.Warningf("[%s] Failed to op: %s", remoteAddr, err)
	}

	ratelimit := rateio.NewSimpleLimiter(3, time.Second*3)
	logger.Debugf("[%s] Joined: %s", remoteAddr, user.Name())

	for {
		line, err := user.ReadLine()
		if err == io.EOF {
			// Closed
			break
		} else if err != nil {
			logger.Errorf("[%s] Terminal reading error: %s", remoteAddr, err)
			break
		}

		err = ratelimit.Count(1)
		if err != nil {
			user.Send(message.NewSystemMsg("Message rejected: Rate limiting is in effect.", user))
			continue
		}
		if len(line) > maxInputLength {
			user.Send(message.NewSystemMsg("Message rejected: Input too long.", user))
			continue
		}
		if line == "" {
			// Silently ignore empty lines.
			continue
		}

		m := message.ParseInput(line, user)

		// FIXME: Any reason to use h.room.Send(m) instead?
		h.HandleMsg(m)

		cmd := m.Command()
		if cmd == "/nick" || cmd == "/theme" {
			// Hijack /nick command to update terminal synchronously. Wouldn't
			// work if we use h.room.Send(m) above.
			//
			// FIXME: This is hacky, how do we improve the API to allow for
			// this? Chat module shouldn't know about terminals.
			term.SetPrompt(user.Prompt())
			user.SetHighlight(user.Name())
		}
	}

	err = h.Leave(user)
	if err != nil {
		logger.Errorf("[%s] Failed to leave: %s", remoteAddr, err)
		return
	}
	logger.Debugf("[%s] Leaving: %s", remoteAddr, user.Name())
}

// Serve our chat room onto the listener
func (h *Host) Serve() {
	h.listener.HandlerFunc = h.Connect
	h.listener.Serve()
}

func (h *Host) completeName(partial string) string {
	names := h.NamesPrefix(partial)
	if len(names) == 0 {
		// Didn't find anything
		return ""
	}

	return names[len(names)-1]
}

func (h *Host) completeCommand(partial string) string {
	for cmd := range h.commands {
		if strings.HasPrefix(cmd, partial) {
			return cmd
		}
	}
	return ""
}

// AutoCompleteFunction returns a callback for terminal autocompletion
func (h *Host) AutoCompleteFunction(u User) func(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
	return func(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
		if key != 9 {
			return
		}

		if line == "" || strings.HasSuffix(line[:pos], " ") {
			// Don't autocomplete spaces.
			return
		}

		fields := strings.Fields(line[:pos])
		isFirst := len(fields) < 2
		partial := ""
		if len(fields) > 0 {
			partial = fields[len(fields)-1]
		}
		posPartial := pos - len(partial)

		var completed string
		if isFirst && strings.HasPrefix(partial, "/") {
			// Command
			completed = h.completeCommand(partial)
			if completed == "/reply" {
				replyTo := u.ReplyTo()
				if replyTo != nil {
					name := replyTo.Name()
					_, found := h.GetUser(name)
					if found {
						completed = "/msg " + name
					} else {
						u.SetReplyTo(nil)
					}
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

// GetUser returns a message.User based on a name.
func (h *Host) GetUser(name string) (User, bool) {
	m, ok := h.MemberByID(name)
	if !ok {
		return nil, false
	}
	u, ok := m.Member.(User)
	return u, ok
}

// InitCommands adds host-specific commands to a Commands container. These will
// override any existing commands.
func (h *Host) InitCommands(c *chat.Commands) {
	c.Add(chat.Command{
		Prefix:     "/msg",
		PrefixHelp: "USER MESSAGE",
		Help:       "Send MESSAGE to USER.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
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

			m := message.NewPrivateMsg(strings.Join(args[1:], " "), msg.From(), target)
			room.Send(&m)

			txt := fmt.Sprintf("[Sent PM to %s]", target.Name())
			ms := message.NewSystemMsg(txt, msg.From())
			room.Send(ms)
			target.SetReplyTo(msg.From())
			return nil
		},
	})

	c.Add(chat.Command{
		Prefix:     "/reply",
		PrefixHelp: "MESSAGE",
		Help:       "Reply with MESSAGE to the previous private message.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			args := msg.Args()
			switch len(args) {
			case 0:
				return errors.New("must specify message")
			}

			target := msg.From().(Member).ReplyTo()
			if target == nil {
				return errors.New("no message to reply to")
			}

			name := target.Name()
			_, found := h.GetUser(name)
			if !found {
				return errors.New("user not found")
			}

			m := message.NewPrivateMsg(strings.Join(args, " "), msg.From(), target)
			room.Send(&m)

			txt := fmt.Sprintf("[Sent PM to %s]", name)
			ms := message.NewSystemMsg(txt, msg.From())
			room.Send(ms)
			return nil
		},
	})

	c.Add(chat.Command{
		Prefix:     "/whois",
		PrefixHelp: "USER",
		Help:       "Information about USER.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			args := msg.Args()
			if len(args) == 0 {
				return errors.New("must specify user")
			}

			target, ok := h.GetUser(args[0])
			if !ok {
				return errors.New("user not found")
			}

			var whois string
			switch room.IsOp(msg.From()) {
			case true:
				whois = whoisAdmin(target)
			case false:
				whois = whoisPublic(target)
			}
			room.Send(message.NewSystemMsg(whois, msg.From()))

			return nil
		},
	})

	// Hidden commands
	c.Add(chat.Command{
		Prefix: "/version",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			room.Send(message.NewSystemMsg(h.Version, msg.From()))
			return nil
		},
	})

	timeStarted := time.Now()
	c.Add(chat.Command{
		Prefix: "/uptime",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			room.Send(message.NewSystemMsg(humanize.Time(timeStarted), msg.From()))
			return nil
		},
	})

	c.Add(chat.Command{
		Op:         true,
		Prefix:     "/op",
		PrefixHelp: "USER [DURATION]",
		Help:       "Set USER as admin.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			if !room.IsOp(msg.From()) {
				return errors.New("must be op")
			}

			args := msg.Args()
			if len(args) == 0 {
				return errors.New("must specify user")
			}

			var until time.Duration = 0
			if len(args) > 1 {
				until, _ = time.ParseDuration(args[1])
			}

			user, ok := h.GetUser(args[0])
			if !ok {
				return errors.New("user not found")
			}
			if until != 0 {
				room.Ops.Add(set.Expire(set.Keyize(user.ID()), until))
			} else {
				room.Ops.Add(set.Keyize(user.ID()))
			}

			// TODO: Add pubkeys to op
			/*
				for _, conn := range user.Connections() {
					h.auth.Op(conn.PublicKey(), until)
				}
			*/

			body := fmt.Sprintf("Made op by %s.", msg.From().Name())
			room.Send(message.NewSystemMsg(body, user))

			return nil
		},
	})

	// Op commands
	c.Add(chat.Command{
		Op:         true,
		Prefix:     "/ban",
		PrefixHelp: "USER [DURATION]",
		Help:       "Ban USER from the server.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
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

			var until time.Duration = 0
			if len(args) > 1 {
				until, _ = time.ParseDuration(args[1])
			}

			for _, conn := range target.Connections() {
				h.auth.Ban(conn.PublicKey(), until)
				h.auth.BanAddr(conn.RemoteAddr(), until)
			}

			body := fmt.Sprintf("%s was banned by %s.", target.Name(), msg.From().Name())
			room.Send(message.NewAnnounceMsg(body))
			target.Close()

			logger.Debugf("Banned: \n-> %s", whoisAdmin(target))

			return nil
		},
	})

	c.Add(chat.Command{
		Op:         true,
		Prefix:     "/kick",
		PrefixHelp: "USER",
		Help:       "Kick USER from the server.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
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
			room.Send(message.NewAnnounceMsg(body))
			target.(io.Closer).Close()
			return nil
		},
	})

	c.Add(chat.Command{
		Op:         true,
		Prefix:     "/motd",
		PrefixHelp: "[MESSAGE]",
		Help:       "Set a new MESSAGE of the day, print the current motd without parameters.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			args := msg.Args()
			user := msg.From()

			h.mu.Lock()
			motd := h.motd
			h.mu.Unlock()

			if len(args) == 0 {
				room.Send(message.NewSystemMsg(motd, user))
				return nil
			}
			if !room.IsOp(user) {
				return errors.New("must be OP to modify the MOTD")
			}

			motd = strings.Join(args, " ")
			h.SetMotd(motd)
			fromMsg := fmt.Sprintf("New message of the day set by %s:", msg.From().Name())
			room.Send(message.NewAnnounceMsg(fromMsg + message.Newline + "-> " + motd))

			return nil
		},
	})

}
