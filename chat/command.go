package chat

// FIXME: Would be sweet if we could piggyback on a cli parser or something.

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/set"
)

// The error returned when an invalid command is issued.
var ErrInvalidCommand = errors.New("invalid command")

// The error returned when a command is given without an owner.
var ErrNoOwner = errors.New("command without owner")

// The error returned when a command is performed without the necessary number
// of arguments.
var ErrMissingArg = errors.New("missing argument")

// The error returned when a command is added without a prefix.
var ErrMissingPrefix = errors.New("command missing prefix")

// Command is a definition of a handler for a command.
type Command struct {
	// The command's key, such as /foo
	Prefix string
	// Extra help regarding arguments
	PrefixHelp string
	// If omitted, command is hidden from /help
	Help    string
	Handler func(*Room, message.CommandMsg) error
	// Command requires Op permissions
	Op bool
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
			newID := SanitizeName(args[0])
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
			// TODO: colorize
			names := room.NamesPrefix("")
			body := fmt.Sprintf("%d connected: %s", len(names), strings.Join(names, ", "))
			room.Send(message.NewSystemMsg(body, msg.From()))
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
				var output bytes.Buffer
				fmt.Fprintf(&output, "Current theme: %s%s", theme, message.Newline)
				fmt.Fprintf(&output, "   Themes available: ")
				for i, t := range message.Themes {
					fmt.Fprintf(&output, t.ID())
					if i < len(message.Themes)-1 {
						fmt.Fprintf(&output, ", ")
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
		Help:   "Raise your arms in the air",
		Handler: func(room *Room, msg message.CommandMsg) error {
			body := strings.TrimLeft(msg.Body(), "/shrug")
			room.Send(message.NewPublicMsg(body + "¯\\_(ツ)_/¯", msg.From()))
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
}
