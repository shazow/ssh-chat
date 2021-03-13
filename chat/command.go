package chat

// FIXME: Would be sweet if we could piggyback on a cli parser or something.

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/internal/sanitize"
	"github.com/shazow/ssh-chat/set"
)

// ErrInvalidCommand is the error returned when an invalid command is issued.
var ErrInvalidCommand = errors.New("invalid command")

// ErrNoOwner is the error returned when a command is given without an owner.
var ErrNoOwner = errors.New("command without owner")

// ErrMissingArg is the error returned when a command is performed without the necessary
// number of arguments.
var ErrMissingArg = errors.New("missing argument")

// ErrMissingPrefix is the error returned when a command is added without a prefix.
var ErrMissingPrefix = errors.New("command missing prefix")

// Command is a definition of a handler for a command.
type Command struct {
	Prefix     string // The command's key, such as /foo
	PrefixHelp string // Extra help regarding arguments
	Help       string // help text, if omitted, command is hidden from /help
	Op         bool   // does the command require Op permissions?

	// Handler for the command
	Handler func(*Room, message.CommandMsg) error
}

// Commands is a registry of available commands.
type Commands map[string]*Command

// Add will register a command. If help string is empty, it will be hidden from
// Help().
func (c Commands) Add(cmd Command) error {
	if cmd.Prefix == "" {
		return ErrMissingPrefix
	}

	c[cmd.Prefix] = &cmd
	return nil
}

// Alias will add another command for the same handler, won't get added to help.
func (c Commands) Alias(command string, alias string) error {
	cmd, ok := c[command]
	if !ok {
		return ErrInvalidCommand
	}
	c[alias] = cmd
	return nil
}

// Run executes a command message.
func (c Commands) Run(room *Room, msg message.CommandMsg) error {
	if msg.From() == nil {
		return ErrNoOwner
	}

	cmd, ok := c[msg.Command()]
	if !ok {
		return ErrInvalidCommand
	}

	return cmd.Handler(room, msg)
}

// Help will return collated help text as one string.
func (c Commands) Help(showOp bool) string {
	// Filter by op
	op := []*Command{}
	normal := []*Command{}
	for _, cmd := range c {
		if cmd.Op {
			op = append(op, cmd)
		} else {
			normal = append(normal, cmd)
		}
	}
	help := "Available commands:" + message.Newline + NewCommandsHelp(normal).String()
	if showOp {
		help += message.Newline + "-> Operator commands:" + message.Newline + NewCommandsHelp(op).String()
	}
	return help
}

var timeformatDatetime = "2006-01-02 15:04:05"

var timeformatTime = "15:04"

var defaultCommands *Commands

func init() {
	defaultCommands = &Commands{}
	InitCommands(defaultCommands)
}

// InitCommands injects default commands into a Commands registry.
func InitCommands(c *Commands) {
	c.Add(Command{
		Prefix: "/help",
		Handler: func(room *Room, msg message.CommandMsg) error {
			op := room.IsOp(msg.From())
			room.Send(message.NewSystemMsg(room.commands.Help(op), msg.From()))
			return nil
		},
	})

	c.Add(Command{
		Prefix: "/me",
		Handler: func(room *Room, msg message.CommandMsg) error {
			me := strings.TrimLeft(msg.Body(), "/me")
			if me == "" {
				me = "is at a loss for words."
			} else {
				me = me[1:]
			}

			room.Send(message.NewEmoteMsg(me, msg.From()))
			return nil
		},
	})

	c.Add(Command{
		Prefix: "/exit",
		Help:   "Exit the chat.",
		Handler: func(room *Room, msg message.CommandMsg) error {
			msg.From().Close()
			return nil
		},
	})
	c.Alias("/exit", "/quit")

	c.Add(Command{
		Prefix:     "/nick",
		PrefixHelp: "NAME",
		Help:       "Rename yourself.",
		Handler: func(room *Room, msg message.CommandMsg) error {
			args := msg.Args()
			if len(args) != 1 {
				return ErrMissingArg
			}
			u := msg.From()

			member, ok := room.MemberByID(u.ID())
			if !ok {
				return errors.New("failed to find member")
			}

			oldID := member.ID()
			newID := sanitize.Name(args[0])
			if newID == oldID {
				return errors.New("new name is the same as the original")
			}
			member.SetID(newID)
			err := room.Rename(oldID, member)
			if err != nil {
				member.SetID(oldID)
				return err
			}
			return nil
		},
	})

	c.Add(Command{
		Prefix: "/names",
		Help:   "List users who are connected.",
		Handler: func(room *Room, msg message.CommandMsg) error {
			theme := msg.From().Config().Theme

			colorize := func(u *message.User) string {
				return theme.ColorName(u)
			}

			if theme == nil {
				colorize = func(u *message.User) string {
					return u.Name()
				}
			}

			names := room.Members.ListPrefix("")
			sort.Slice(names, func(i, j int) bool { return names[i].Key() < names[j].Key() })
			activeColNames := []string{}
			awayColNames := []string{}
			for _, uname := range names {
				user := uname.Value().(*Member).User
				colUser := colorize(user)
				if isAway, _, _ := user.GetAway(); isAway {
					awayColNames = append(awayColNames, colUser)
				} else {
					activeColNames = append(activeColNames, colUser)
				}
			}
			numPeople := strconv.Itoa(len(names))
			activePeople := strings.Join(activeColNames, ", ")

			if len(awayColNames) > 0 {
				awayPeople := strings.Join(awayColNames, ",")
				room.Send(message.NewSystemMsgP(msg.From(), numPeople, " connected: ", activePeople, "; away: ", awayPeople))
				return nil
			}

			room.Send(message.NewSystemMsgP(msg.From(), numPeople, " connected: ", activePeople))
			return nil
		},
	})
	c.Alias("/names", "/list")

	c.Add(Command{
		Prefix:     "/theme",
		PrefixHelp: "[colors|...]",
		Help:       "Set your color theme.",
		Handler: func(room *Room, msg message.CommandMsg) error {
			user := msg.From()
			args := msg.Args()
			cfg := user.Config()
			if len(args) == 0 {
				theme := "plain"
				if cfg.Theme != nil {
					theme = cfg.Theme.ID()
				}
				var output strings.Builder
				fmt.Fprintf(&output, "Current theme: %s%s", theme, message.Newline)
				fmt.Fprintf(&output, "   Themes available: ")

				for i, t := range message.Themes {
					output.WriteString(t.ID())
					if i < len(message.Themes)-1 {
						output.WriteString(", ")
					}
				}
				room.Send(message.NewSystemMsg(output.String(), user))
				return nil
			}

			id := args[0]
			for _, t := range message.Themes {
				if t.ID() == id {
					cfg.Theme = &t
					user.SetConfig(cfg)
					body := fmt.Sprintf("Set theme: %s", id)
					room.Send(message.NewSystemMsg(body, user))
					return nil
				}
			}
			return errors.New("theme not found")
		},
	})

	c.Add(Command{
		Prefix: "/quiet",
		Help:   "Silence room announcements.",
		Handler: func(room *Room, msg message.CommandMsg) error {
			u := msg.From()
			cfg := u.Config()
			cfg.Quiet = !cfg.Quiet
			u.SetConfig(cfg)

			var body string
			if cfg.Quiet {
				body = "Quiet mode is toggled ON"
			} else {
				body = "Quiet mode is toggled OFF"
			}
			room.Send(message.NewSystemMsg(body, u))
			return nil
		},
	})

	c.Add(Command{
		Prefix:     "/slap",
		PrefixHelp: "NAME",
		Handler: func(room *Room, msg message.CommandMsg) error {
			var me string
			args := msg.Args()
			if len(args) == 0 {
				me = "slaps themselves around a bit with a large trout."
			} else {
				me = fmt.Sprintf("slaps %s around a bit with a large trout.", strings.Join(args, " "))
			}

			room.Send(message.NewEmoteMsg(me, msg.From()))
			return nil
		},
	})

	c.Add(Command{
		Prefix: "/shrug",
		Handler: func(room *Room, msg message.CommandMsg) error {
			room.Send(message.NewEmoteMsg(`¯\_(ツ)_/¯`, msg.From()))
			return nil
		},
	})

	c.Add(Command{
		Prefix:     "/timestamp",
		PrefixHelp: "[time|datetime]",
		Help:       "Prefix messages with a timestamp. You can also provide the UTC offset: /timestamp time +5h45m",
		Handler: func(room *Room, msg message.CommandMsg) error {
			u := msg.From()
			cfg := u.Config()

			args := msg.Args()
			mode := ""
			if len(args) >= 1 {
				mode = args[0]
			}
			if len(args) >= 2 {
				// FIXME: This is an annoying format to demand from users, but
				// hopefully we can make it a non-primary flow if we add GeoIP
				// someday.
				offset, err := time.ParseDuration(args[1])
				if err != nil {
					return err
				}
				cfg.Timezone = time.FixedZone("", int(offset.Seconds()))
			}

			switch mode {
			case "time":
				cfg.Timeformat = &timeformatTime
			case "datetime":
				cfg.Timeformat = &timeformatDatetime
			case "":
				// Toggle
				if cfg.Timeformat != nil {
					cfg.Timeformat = nil
				} else {
					cfg.Timeformat = &timeformatTime
				}
			case "off":
				cfg.Timeformat = nil
			default:
				return errors.New("timestamp value must be one of: time, datetime, off")
			}

			u.SetConfig(cfg)

			var body string
			if cfg.Timeformat != nil {
				if cfg.Timezone != nil {
					tzname := time.Now().In(cfg.Timezone).Format("MST")
					body = fmt.Sprintf("Timestamp is toggled ON, timezone is %q", tzname)
				} else {
					body = "Timestamp is toggled ON, timezone is UTC"
				}
			} else {
				body = "Timestamp is toggled OFF"
			}
			room.Send(message.NewSystemMsg(body, u))
			return nil
		},
	})

	c.Add(Command{
		Prefix:     "/ignore",
		PrefixHelp: "[USER]",
		Help:       "Hide messages from USER, /unignore USER to stop hiding.",
		Handler: func(room *Room, msg message.CommandMsg) error {
			id := strings.TrimSpace(strings.TrimLeft(msg.Body(), "/ignore"))
			if id == "" {
				// Print ignored names, if any.
				var names []string
				msg.From().Ignored.Each(func(_ string, item set.Item) error {
					names = append(names, item.Key())
					return nil
				})

				var systemMsg string
				if len(names) == 0 {
					systemMsg = "0 users ignored."
				} else {
					systemMsg = fmt.Sprintf("%d ignored: %s", len(names), strings.Join(names, ", "))
				}

				room.Send(message.NewSystemMsg(systemMsg, msg.From()))
				return nil
			}

			if id == msg.From().ID() {
				return errors.New("cannot ignore self")
			}
			target, ok := room.MemberByID(id)
			if !ok {
				return fmt.Errorf("user not found: %s", id)
			}

			err := msg.From().Ignored.Add(set.Itemize(id, target))
			if err == set.ErrCollision {
				return fmt.Errorf("user already ignored: %s", id)
			} else if err != nil {
				return err
			}

			room.Send(message.NewSystemMsg(fmt.Sprintf("Ignoring: %s", target.Name()), msg.From()))
			return nil
		},
	})

	c.Add(Command{
		Prefix:     "/unignore",
		PrefixHelp: "USER",
		Handler: func(room *Room, msg message.CommandMsg) error {
			id := strings.TrimSpace(strings.TrimLeft(msg.Body(), "/unignore"))
			if id == "" {
				return errors.New("must specify user")
			}

			if err := msg.From().Ignored.Remove(id); err != nil {
				return err
			}

			room.Send(message.NewSystemMsg(fmt.Sprintf("No longer ignoring: %s", id), msg.From()))
			return nil
		},
	})

	c.Add(Command{
		Prefix:     "/focus",
		PrefixHelp: "[USER ...]",
		Help:       "Only show messages from focused users, or $ to reset.",
		Handler: func(room *Room, msg message.CommandMsg) error {
			ids := strings.TrimSpace(strings.TrimLeft(msg.Body(), "/focus"))
			if ids == "" {
				// Print focused names, if any.
				var names []string
				msg.From().Focused.Each(func(_ string, item set.Item) error {
					names = append(names, item.Key())
					return nil
				})

				var systemMsg string
				if len(names) == 0 {
					systemMsg = "Unfocused."
				} else {
					systemMsg = fmt.Sprintf("Focusing on %d users: %s", len(names), strings.Join(names, ", "))
				}

				room.Send(message.NewSystemMsg(systemMsg, msg.From()))
				return nil
			}

			n := msg.From().Focused.Clear()
			if ids == "$" {
				room.Send(message.NewSystemMsg(fmt.Sprintf("Removed focus from %d users.", n), msg.From()))
				return nil
			}

			var focused []string
			for _, name := range strings.Split(ids, " ") {
				id := sanitize.Name(name)
				if id == "" {
					continue // Skip
				}
				focused = append(focused, id)
				if err := msg.From().Focused.Set(set.Itemize(id, set.ZeroValue)); err != nil {
					return err
				}
			}
			room.Send(message.NewSystemMsg(fmt.Sprintf("Focusing: %s", strings.Join(focused, ", ")), msg.From()))
			return nil
		},
	})

	c.Add(Command{
		Prefix:     "/away",
		PrefixHelp: "[AWAY MESSAGE]",
		Handler: func(room *Room, msg message.CommandMsg) error {
			awayMsg := strings.TrimSpace(strings.TrimLeft(msg.Body(), "/away"))
			isAway, _, _ := msg.From().GetAway()
			if awayMsg == "" {
				if isAway {
					msg.From().SetActive()
					room.Send(message.NewSystemMsg("You are marked as active, welcome back!", msg.From()))
					room.Send(message.NewEmoteMsg("is back", msg.From()))
					return nil
				}

				room.Send(message.NewSystemMsg("Not away. Add an away message to set away.", msg.From()))
				return nil
			}

			msg.From().SetAway(awayMsg)
			room.Send(message.NewSystemMsg("You are marked as away, enjoy your excursion!", msg.From()))

			room.Send(message.NewEmoteMsg("has gone away: "+awayMsg, msg.From()))
			return nil
		},
	})
}
