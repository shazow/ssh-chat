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

// GetPrompt will render the terminal prompt string based on the user.
func GetPrompt(user *message.User) string {
	name := user.Name()
	cfg := user.Config()
	if cfg.Theme != nil {
		name = cfg.Theme.ColorName(user)
	}
	return fmt.Sprintf("[%s:%s] ", name, user.Chat())
}

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

	mu   sync.Mutex
	motd string
	// start private mode
	private map[string]*message.User

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

func (h *Host) isOp(conn sshd.Connection) bool {
	key := conn.PublicKey()
	if key == nil {
		return false
	}
	return h.auth.IsOp(key)
}

func (h *Host) isMaster(conn sshd.Connection) bool {
	key := conn.PublicKey()
	if key == nil {
		return false
	}
	return h.auth.IsMaster(key)
}

// Connect a specific Terminal to this host and its room.
func (h *Host) Connect(term *sshd.Terminal) {
	id := NewIdentity(term.Conn)
	user := message.NewUserScreen(id, term)
	cfg := user.Config()
	cfg.Theme = &h.theme
	user.SetConfig(cfg)
	go user.Consume()

	// Close term once user is closed.
	defer user.Close()
	defer term.Close()

	h.mu.Lock()
	motd := h.motd
	count := h.count
	h.count++
	h.mu.Unlock()

	// Send MOTD
	if motd != "" {
		user.Send(message.NewAnnounceMsg(motd))
	}

	member, err := h.Join(user)
	if err != nil {
		// Try again...
		id.SetName(fmt.Sprintf("Guest%d", count))
		member, err = h.Join(user)
	}
	if err != nil {
		logger.Errorf("[%s] Failed to join: %s", term.Conn.RemoteAddr(), err)
		return
	}

	// Successfully joined.
	user.SetChat("general")
	term.SetPrompt(GetPrompt(user))
	term.AutoCompleteCallback = h.AutoCompleteFunction(user)
	user.SetHighlight(user.Name())

	// Should the user be op'd on join?
	if h.isOp(term.Conn) {
		h.Room.Ops.Add(set.Itemize(member.ID(), member))
	}
	if h.isMaster(term.Conn) {
		h.Room.Masters.Add(set.Itemize(member.ID(), member))
	}
	ratelimit := rateio.NewSimpleLimiter(3, time.Second*3)

	logger.Debugf("[%s] Joined: %s", term.Conn.RemoteAddr(), user.Name())

	var args []string
	h.private = make(map[string]*message.User)
	for {
		line, err := term.ReadLine()
		if err == io.EOF {
			// Closed
			break
		} else if err != nil {
			logger.Errorf("[%s] Terminal reading error: %s", term.Conn.RemoteAddr(), err)
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

		switch c := m.(type) {
		case *message.CommandMsg:
			args = c.Args()
			h.HandleMsg(m)
		default:
			to, ok := h.private[user.Name()]
			if ok {
				m = message.NewPrivateMsg(
					m.String(), user, to,
				)
			}
			h.HandleMsg(m)
		}

		if cmd := m.Command(); len(cmd) > 0 {
			switch cmd[1:] {
			case "private":
				if len(args) > 0 {
					user.SetChat(args[0])
				}
			case "endprivate":
				user.SetChat("general")
			case "nick":
			case "theme":
			}
			term.SetPrompt(GetPrompt(user))
			user.SetHighlight(user.Name())
		}
		args = nil
	}

	err = h.Leave(user)
	if err != nil {
		logger.Errorf("[%s] Failed to leave: %s", term.Conn.RemoteAddr(), err)
		return
	}
	logger.Debugf("[%s] Leaving: %s", term.Conn.RemoteAddr(), user.Name())
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
func (h *Host) AutoCompleteFunction(u *message.User) func(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
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
func (h *Host) GetUser(name string) (*message.User, bool) {
	m, ok := h.MemberByID(name)
	if !ok {
		return nil, false
	}
	return m.User, true
}

// InitCommands adds host-specific commands to a Commands container. These will
// override any existing commands.
func (h *Host) InitCommands(c *chat.Commands) {
	c.Add(chat.Command{
		Prefix:     "msg",
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
		Prefix:     "private",
		PrefixHelp: "USER",
		Help:       "Start private chat with USER",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			args := msg.Args()
			if len(args) == 0 {
				return errors.New("must specify user")
			}

			target, ok := h.GetUser(args[0])
			if !ok {
				return errors.New("user not found")
			}

			h.private[msg.From().Name()] = target

			txt := fmt.Sprintf("[Private mode started with %s]", target.Name())
			ms := message.NewSystemMsg(txt, msg.From())
			room.Send(ms)
			target.SetReplyTo(msg.From())
			return nil
		},
	})
	c.Alias("p", "private")

	c.Add(chat.Command{
		Admin:  true,
		Prefix: "welcome",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			if !room.IsMaster(msg.From()) {
				return errors.New("must be admin")
			}

			room.Send(message.NewMsg(h.motd))
			return nil
		},
	})

	c.Add(chat.Command{
		Prefix: "endprivate",
		Help:   "Stop private chat",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			delete(h.private, msg.From().Name())

			txt := "[Private mode ended]"
			ms := message.NewSystemMsg(txt, msg.From())
			room.Send(ms)
			return nil
		},
	})

	c.Add(chat.Command{
		Prefix:     "reply",
		PrefixHelp: "MESSAGE",
		Help:       "Reply with MESSAGE to the previous private message.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			args := msg.Args()
			switch len(args) {
			case 0:
				return errors.New("must specify message")
			}

			target := msg.From().ReplyTo()
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
		Prefix:     "whois",
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

			id := target.Identifier.(*Identity)
			var whois string
			switch room.IsMaster(msg.From()) {
			case true:
				whois = id.WhoisMaster()
			case false:
				if room.IsOp(msg.From()) {
					whois = id.WhoisAdmin()
				} else {
					whois = id.Whois()
				}
			}
			room.Send(message.NewSystemMsg(whois, msg.From()))

			return nil
		},
	})

	// Hidden commands
	c.Add(chat.Command{
		Prefix: "version",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			room.Send(message.NewSystemMsg(h.Version, msg.From()))
			return nil
		},
	})

	timeStarted := time.Now()
	c.Add(chat.Command{
		Prefix: "uptime",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			room.Send(message.NewSystemMsg(humanize.Time(timeStarted), msg.From()))
			return nil
		},
	})

	// Op commands
	c.Add(chat.Command{
		Op:         true,
		Admin:      true,
		Prefix:     "kick",
		PrefixHelp: "USER",
		Help:       "Kick USER from the server.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			if !room.IsOp(msg.From()) && !room.IsMaster(msg.From()) {
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

			if room.IsMaster(target) {
				return errors.New("you cannot kick master")
			}

			body := fmt.Sprintf("%s was kicked by %s.", target.Name(), msg.From().Name())
			room.Send(message.NewAnnounceMsg(body))
			target.Close()
			return nil
		},
	})

	c.Add(chat.Command{
		Admin:      true,
		Op:         true,
		Prefix:     "ban",
		PrefixHelp: "USER [DURATION]",
		Help:       "Ban USER from the server.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			// TODO: Would be nice to specify what to ban. Key? Ip? etc.
			if !room.IsMaster(msg.From()) {
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

			if room.IsMaster(target) {
				return errors.New("you cannot ban master.")
			}

			var until time.Duration = 0
			if len(args) > 1 {
				until, _ = time.ParseDuration(args[1])
			}

			id := target.Identifier.(*Identity)
			h.auth.Ban(id.PublicKey(), until)
			h.auth.BanAddr(id.RemoteAddr(), until)

			body := fmt.Sprintf("%s was banned by %s.", target.Name(), msg.From().Name())
			room.Send(message.NewAnnounceMsg(body))
			target.Close()

			logger.Debugf("Banned: \n-> %s", id.Whois())

			return nil
		},
	})

	c.Add(chat.Command{
		Admin:      true,
		Op:         true,
		Prefix:     "motd",
		PrefixHelp: "[MESSAGE]",
		Help:       "Set a new MESSAGE of the day, print the current motd without parameters.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			user := msg.From()
			if !room.IsMaster(user) && !room.IsOp(user) {
				return errors.New("must be OP to modify the MOTD")
			}

			args := msg.Args()

			h.mu.Lock()
			motd := h.motd
			h.mu.Unlock()

			if len(args) == 0 {
				room.Send(message.NewSystemMsg(motd, user))
				return nil
			}

			motd = strings.Join(args, " ")
			h.SetMotd(motd)
			fromMsg := fmt.Sprintf("New message of the day set by %s:", msg.From().Name())
			room.Send(message.NewAnnounceMsg(fromMsg + message.Newline + "-> " + motd))

			return nil
		},
	})

	c.Add(chat.Command{
		Admin:      true,
		Prefix:     "op",
		PrefixHelp: "USER [DURATION]",
		Help:       "Set USER as admin.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			if !room.IsMaster(msg.From()) {
				return errors.New("must be admin")
			}

			args := msg.Args()
			if len(args) == 0 {
				return errors.New("must specify user")
			}

			var until time.Duration = 0
			if len(args) > 1 {
				until, _ = time.ParseDuration(args[1])
			}

			member, ok := room.MemberByID(args[0])
			if !ok {
				return errors.New("user not found")
			}
			room.Ops.Add(set.Itemize(member.ID(), member))

			id := member.Identifier.(*Identity)
			h.auth.Op(id.PublicKey(), until)

			body := fmt.Sprintf("Made op by %s.", msg.From().Name())
			room.Send(message.NewSystemMsg(body, member.User))

			return nil
		},
	})

	c.Add(chat.Command{
		Admin:      true,
		Prefix:     "delop",
		PrefixHelp: "USER",
		Help:       "Remove USER as admin.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			if !room.IsMaster(msg.From()) {
				return errors.New("must be master")
			}

			args := msg.Args()
			if len(args) == 0 {
				return errors.New("must specify user")
			}

			member, ok := room.MemberByID(args[0])
			if !ok {
				return errors.New("user not found")
			}
			err := room.Ops.Remove(args[0])
			if err != nil {
				return err
			}

			body := fmt.Sprintf("Deleted op by %s.", msg.From().Name())
			room.Send(message.NewSystemMsg(body, member.User))

			return nil
		},
	})
}
