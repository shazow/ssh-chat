package sshchat

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/shazow/rateio"
	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/internal/humantime"
	"github.com/shazow/ssh-chat/internal/sanitize"
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
	return fmt.Sprintf("[%s] ", name)
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

	mu    sync.Mutex
	motd  string
	count int

	// GetMOTD is used to reload the motd from an external source
	GetMOTD func() (string, error)
	// OnUserJoined is used to notify when a user joins a host
	OnUserJoined func(*message.User)
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
// TODO: Change to SetMOTD
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

// Connect a specific Terminal to this host and its room.
func (h *Host) Connect(term *sshd.Terminal) {
	id := NewIdentity(term.Conn)
	user := message.NewUserScreen(id, term)
	user.OnChange = func() {
		term.SetPrompt(GetPrompt(user))
		user.SetHighlight(user.ID())
	}
	cfg := user.Config()

	apiMode := strings.ToLower(term.Term()) == "bot"

	if apiMode {
		cfg.Theme = message.MonoTheme
		cfg.Echo = false
	} else {
		term.SetEnterClear(true) // We provide our own echo rendering
		cfg.Theme = &h.theme
	}

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

	// Load user config overrides from ENV
	// TODO: Would be nice to skip the command parsing pipeline just to load
	// config values. Would need to factor out some command handler logic into
	// accessible helpers.
	env := term.Env()
	for _, e := range env {
		switch e.Key {
		case "SSHCHAT_TIMESTAMP":
			if e.Value != "" && e.Value != "0" {
				cmd := "/timestamp"
				if e.Value != "1" {
					cmd += " " + e.Value
				}
				if msg, ok := message.NewPublicMsg(cmd, user).ParseCommand(); ok {
					h.Room.HandleMsg(msg)
				}
			}
		case "SSHCHAT_THEME":
			cmd := "/theme " + e.Value
			if msg, ok := message.NewPublicMsg(cmd, user).ParseCommand(); ok {
				h.Room.HandleMsg(msg)
			}
		}
	}

	// Successfully joined.
	if !apiMode {
		term.SetPrompt(GetPrompt(user))
		term.AutoCompleteCallback = h.AutoCompleteFunction(user)
		user.SetHighlight(user.Name())
	}

	// Should the user be op'd on join?
	if h.isOp(term.Conn) {
		member.IsOp = true
	}
	ratelimit := rateio.NewSimpleLimiter(3, time.Second*3)

	logger.Debugf("[%s] Joined: %s", term.Conn.RemoteAddr(), user.Name())

	if h.OnUserJoined != nil {
		h.OnUserJoined(user)
	}

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
			term.Write([]byte{})
			continue
		}

		m := message.ParseInput(line, user)

		if !apiMode {
			if m, ok := m.(*message.CommandMsg); ok {
				// Other messages render themselves by the room, commands we'll
				// have to re-echo ourselves manually.
				user.HandleMsg(m)
			}
		}

		// FIXME: Any reason to use h.room.Send(m) instead?
		h.HandleMsg(m)

		if apiMode {
			// Skip the remaining rendering workarounds
			continue
		}
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

func (h *Host) completeName(partial string, skipName string) string {
	names := h.NamesPrefix(partial)
	if len(names) == 0 {
		// Didn't find anything
		return ""
	} else if name := names[0]; name != skipName {
		// First name is not the skipName, great
		return name
	} else if len(names) > 1 {
		// Next candidate
		return names[1]
	}
	return ""
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
		if isFirst && strings.HasPrefix(line, "/") {
			// Command
			completed = h.completeCommand(partial)
			if completed == "/reply" {
				replyTo := u.ReplyTo()
				if replyTo != nil {
					name := replyTo.ID()
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
			completed = h.completeName(partial, u.Name())
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
	sendPM := func(room *chat.Room, msg string, from *message.User, target *message.User) error {
		m := message.NewPrivateMsg(msg, from, target)
		room.Send(&m)

		txt := fmt.Sprintf("[Sent PM to %s]", target.Name())
		if isAway, _, awayReason := target.GetAway(); isAway {
			txt += " Away: " + awayReason
		}
		sysMsg := message.NewSystemMsg(txt, from)
		room.Send(sysMsg)
		target.SetReplyTo(from)
		return nil
	}

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

			return sendPM(room, strings.Join(args[1:], " "), msg.From(), target)
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

			target := msg.From().ReplyTo()
			if target == nil {
				return errors.New("no message to reply to")
			}

			_, found := h.GetUser(target.ID())
			if !found {
				return errors.New("user not found")
			}

			return sendPM(room, strings.Join(args, " "), msg.From(), target)
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
			id := target.Identifier.(*Identity)
			var whois string
			switch room.IsOp(msg.From()) {
			case true:
				whois = id.WhoisAdmin(room)
			case false:
				whois = id.Whois(room)
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
			room.Send(message.NewSystemMsg(humantime.Since(timeStarted), msg.From()))
			return nil
		},
	})

	// Op commands
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
			target.Close()
			return nil
		},
	})

	c.Add(chat.Command{
		Op:         true,
		Prefix:     "/ban",
		PrefixHelp: "QUERY [DURATION]",
		Help:       "Ban from the server. QUERY can be a username to ban the fingerprint and ip, or quoted \"key=value\" pairs with keys like ip, fingerprint, client.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			// TODO: Would be nice to specify what to ban. Key? Ip? etc.
			if !room.IsOp(msg.From()) {
				return errors.New("must be op")
			}

			args := msg.Args()
			if len(args) == 0 {
				return errors.New("must specify user")
			}

			query := args[0]
			target, ok := h.GetUser(query)
			if !ok {
				query = strings.Join(args, " ")
				if strings.Contains(query, "=") {
					return h.auth.BanQuery(query)
				}
				return errors.New("user not found")
			}

			var until time.Duration
			if len(args) > 1 {
				until, _ = time.ParseDuration(args[1])
			}

			id := target.Identifier.(*Identity)
			h.auth.Ban(id.PublicKey(), until)
			h.auth.BanAddr(id.RemoteAddr(), until)

			body := fmt.Sprintf("%s was banned by %s.", target.Name(), msg.From().Name())
			room.Send(message.NewAnnounceMsg(body))
			target.Close()

			logger.Debugf("Banned: \n-> %s", id.Whois(room))

			return nil
		},
	})

	c.Add(chat.Command{
		Op:     true,
		Prefix: "/banned",
		Help:   "List the current ban conditions.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			if !room.IsOp(msg.From()) {
				return errors.New("must be op")
			}

			bannedIPs, bannedFingerprints, bannedClients := h.auth.Banned()

			buf := bytes.Buffer{}
			fmt.Fprintf(&buf, "Banned:")
			for _, key := range bannedIPs {
				fmt.Fprintf(&buf, "\n   \"ip=%s\"", key)
			}
			for _, key := range bannedFingerprints {
				fmt.Fprintf(&buf, "\n   \"fingerprint=%s\"", key)
			}
			for _, key := range bannedClients {
				fmt.Fprintf(&buf, "\n   \"client=%s\"", key)
			}

			room.Send(message.NewSystemMsg(buf.String(), msg.From()))

			return nil
		},
	})

	c.Add(chat.Command{
		Op:         true,
		Prefix:     "/motd",
		PrefixHelp: "[MESSAGE]",
		Help:       "Set a new MESSAGE of the day, or print the motd if no MESSAGE.",
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

			var err error
			var s string = strings.Join(args, " ")

			if s == "@" {
				if h.GetMOTD == nil {
					return errors.New("motd reload not set")
				}
				if s, err = h.GetMOTD(); err != nil {
					return err
				}
			}

			h.SetMotd(s)
			fromMsg := fmt.Sprintf("New message of the day set by %s:", msg.From().Name())
			room.Send(message.NewAnnounceMsg(fromMsg + message.Newline + "-> " + s))

			return nil
		},
	})

	c.Add(chat.Command{
		Op:         true,
		Prefix:     "/op",
		PrefixHelp: "USER [DURATION|remove]",
		Help:       "Set USER as admin. Duration only applies to pubkey reconnects.",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			if !room.IsOp(msg.From()) {
				return errors.New("must be op")
			}

			args := msg.Args()
			if len(args) == 0 {
				return errors.New("must specify user")
			}

			opValue := true
			var until time.Duration
			if len(args) > 1 {
				if args[1] == "remove" {
					// Expire instantly
					until = time.Duration(1)
					opValue = false
				} else {
					until, _ = time.ParseDuration(args[1])
				}
			}

			member, ok := room.MemberByID(args[0])
			if !ok {
				return errors.New("user not found")
			}
			member.IsOp = opValue

			id := member.Identifier.(*Identity)
			h.auth.Op(id.PublicKey(), until)

			var body string
			if opValue {
				body = fmt.Sprintf("Made op by %s.", msg.From().Name())
			} else {
				body = fmt.Sprintf("Removed op by %s.", msg.From().Name())
			}
			room.Send(message.NewSystemMsg(body, member.User))

			return nil
		},
	})

	c.Add(chat.Command{
		Op:         true,
		Prefix:     "/rename",
		PrefixHelp: "USER NEW_NAME [SYMBOL]",
		Help:       "Rename USER to NEW_NAME, add optional SYMBOL prefix",
		Handler: func(room *chat.Room, msg message.CommandMsg) error {
			if !room.IsOp(msg.From()) {
				return errors.New("must be op")
			}

			args := msg.Args()
			if len(args) < 2 {
				return errors.New("must specify user and new name")
			}

			member, ok := room.MemberByID(args[0])
			if !ok {
				return errors.New("user not found")
			}

			symbolSet := false
			if len(args) == 3 {
				s := args[2]
				if id, ok := member.Identifier.(*Identity); ok {
					id.SetSymbol(s)
				} else {
					return errors.New("user does not support setting symbol")
				}

				body := fmt.Sprintf("Assigned symbol %q by %s.", s, msg.From().Name())
				room.Send(message.NewSystemMsg(body, member.User))
				symbolSet = true
			}

			oldID := member.ID()
			newID := sanitize.Name(args[1])
			if newID == oldID && !symbolSet {
				return errors.New("new name is the same as the original")
			} else if (newID == "" || newID == oldID) && symbolSet {
				if member.User.OnChange != nil {
					member.User.OnChange()
				}
				return nil
			}

			member.SetID(newID)
			err := room.Rename(oldID, member)
			if err != nil {
				member.SetID(oldID)
				return err
			}

			body := fmt.Sprintf("%s was renamed by %s.", oldID, msg.From().Name())
			room.Send(message.NewAnnounceMsg(body))

			return nil
		},
	})

	forConnectedUsers := func(cmd func(*chat.Member, ssh.PublicKey) error) error {
		return h.Members.Each(func(key string, item set.Item) error {
			v := item.Value()
			if v == nil { // expired between Each and here
				return nil
			}
			user := v.(*chat.Member)
			pk := user.Identifier.(*Identity).PublicKey()
			return cmd(user, pk)
		})
	}

	forPubkeyUser := func(args []string, cmd func(ssh.PublicKey)) (errors []string) {
		invalidUsers := []string{}
		invalidKeys := []string{}
		noKeyUsers := []string{}
		var keyType string
		for _, v := range args {
			switch {
			case keyType != "":
				pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyType + " " + v))
				if err == nil {
					cmd(pk)
				} else {
					invalidKeys = append(invalidKeys, keyType+" "+v)
				}
				keyType = ""
			case strings.HasPrefix(v, "ssh-"):
				keyType = v
			default:
				user, ok := h.GetUser(v)
				if ok {
					pk := user.Identifier.(*Identity).PublicKey()
					if pk == nil {
						noKeyUsers = append(noKeyUsers, user.Identifier.Name())
					} else {
						cmd(pk)
					}
				} else {
					invalidUsers = append(invalidUsers, v)
				}
			}
		}
		if len(noKeyUsers) != 0 {
			errors = append(errors, fmt.Sprintf("users without a public key: %v", noKeyUsers))
		}
		if len(invalidUsers) != 0 {
			errors = append(errors, fmt.Sprintf("invalid users: %v", invalidUsers))
		}
		if len(invalidKeys) != 0 {
			errors = append(errors, fmt.Sprintf("invalid keys: %v", invalidKeys))
		}
		return
	}

	allowlistHelptext := []string{
		"Usage: /allowlist help | on | off | add {PUBKEY|USER}... | remove {PUBKEY|USER}... | import [AGE] | reload {keep|flush} | reverify | status",
		"help: this help message",
		"on, off: set allowlist mode (applies to new connections)",
		"add, remove: add or remove keys from the allowlist",
		"import: add all keys of users connected since AGE (default 0) ago to the allowlist",
		"reload: re-read the allowlist file and keep or discard entries in the current allowlist but not in the file",
		"reverify: kick all users not in the allowlist if allowlisting is enabled",
		"status: show status information",
	}

	allowlistImport := func(args []string) (msgs []string, err error) {
		var since time.Duration
		if len(args) > 0 {
			since, err = time.ParseDuration(args[0])
			if err != nil {
				return
			}
		}
		cutoff := time.Now().Add(-since)
		noKeyUsers := []string{}
		forConnectedUsers(func(user *chat.Member, pk ssh.PublicKey) error {
			if user.Joined().Before(cutoff) {
				if pk == nil {
					noKeyUsers = append(noKeyUsers, user.Identifier.Name())
				} else {
					h.auth.Allowlist(pk, 0)
				}
			}
			return nil
		})
		if len(noKeyUsers) != 0 {
			msgs = []string{fmt.Sprintf("users without a public key: %v", noKeyUsers)}
		}
		return
	}

	allowlistReload := func(args []string) error {
		if !(len(args) > 0 && (args[0] == "keep" || args[0] == "flush")) {
			return errors.New("must specify whether to keep or flush current entries")
		}
		if args[0] == "flush" {
			h.auth.allowlist.Clear()
		}
		return h.auth.ReloadAllowlist()
	}

	allowlistReverify := func(room *chat.Room) []string {
		if !h.auth.AllowlistMode() {
			return []string{"allowlist is disabled, so nobody will be kicked"}
		}
		var kicked []string
		forConnectedUsers(func(user *chat.Member, pk ssh.PublicKey) error {
			if h.auth.CheckPublicKey(pk) != nil && !user.IsOp { // we do this check here as well for ops without keys
				kicked = append(kicked, user.Name())
				user.Close()
			}
			return nil
		})
		if kicked != nil {
			room.Send(message.NewAnnounceMsg("Kicked during pubkey reverification: " + strings.Join(kicked, ", ")))
		}
		return nil
	}

	allowlistStatus := func() (msgs []string) {
		if h.auth.AllowlistMode() {
			msgs = []string{"allowlist enabled"}
		} else {
			msgs = []string{"allowlist disabled"}
		}
		allowlistedUsers := []string{}
		allowlistedKeys := []string{}
		h.auth.allowlist.Each(func(key string, item set.Item) error {
			keyFP := item.Key()
			if forConnectedUsers(func(user *chat.Member, pk ssh.PublicKey) error {
				if pk != nil && sshd.Fingerprint(pk) == keyFP {
					allowlistedUsers = append(allowlistedUsers, user.Name())
					return io.EOF
				}
				return nil
			}) == nil {
				// if we land here, the key matches no users
				allowlistedKeys = append(allowlistedKeys, keyFP)
			}
			return nil
		})
		if len(allowlistedUsers) != 0 {
			msgs = append(msgs, "Connected users on the allowlist: "+strings.Join(allowlistedUsers, ", "))
		}
		if len(allowlistedKeys) != 0 {
			msgs = append(msgs, "Keys on the allowlist without connected user: "+strings.Join(allowlistedKeys, ", "))
		}
		return
	}

	c.Add(chat.Command{
		Op:         true,
		Prefix:     "/allowlist",
		PrefixHelp: "COMMAND [ARGS...]",
		Help:       "Modify the allowlist or allowlist state. See /allowlist help for subcommands",
		Handler: func(room *chat.Room, msg message.CommandMsg) (err error) {
			if !room.IsOp(msg.From()) {
				return errors.New("must be op")
			}

			args := msg.Args()
			if len(args) == 0 {
				args = []string{"help"}
			}

			// send exactly one message to preserve order
			var replyLines []string

			switch args[0] {
			case "help":
				replyLines = allowlistHelptext
			case "on":
				h.auth.SetAllowlistMode(true)
			case "off":
				h.auth.SetAllowlistMode(false)
			case "add":
				replyLines = forPubkeyUser(args[1:], func(pk ssh.PublicKey) { h.auth.Allowlist(pk, 0) })
			case "remove":
				replyLines = forPubkeyUser(args[1:], func(pk ssh.PublicKey) { h.auth.Allowlist(pk, 1) })
			case "import":
				replyLines, err = allowlistImport(args[1:])
			case "reload":
				err = allowlistReload(args[1:])
			case "reverify":
				replyLines = allowlistReverify(room)
			case "status":
				replyLines = allowlistStatus()
			default:
				err = errors.New("invalid subcommand: " + args[0])
			}
			if err == nil && replyLines != nil {
				room.Send(message.NewSystemMsg(strings.Join(replyLines, "\r\n"), msg.From()))
			}
			return
		},
	})
}
