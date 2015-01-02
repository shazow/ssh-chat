// FIXME: Would be sweet if we could piggyback on a cli parser or something.
package chat

import (
	"errors"
	"fmt"
	"strings"
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
	Handler func(*Channel, CommandMsg) error
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
func (c Commands) Run(channel *Channel, msg CommandMsg) error {
	if msg.From == nil {
		return ErrNoOwner
	}

	cmd, ok := c[msg.Command()]
	if !ok {
		return ErrInvalidCommand
	}

	return cmd.Handler(channel, msg)
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
	help := "Available commands:" + Newline + NewCommandsHelp(normal).String()
	if showOp {
		help += Newline + "Operator commands:" + Newline + NewCommandsHelp(op).String()
	}
	return help
}

var defaultCommands *Commands

func init() {
	c := Commands{}

	c.Add(Command{
		Prefix: "/help",
		Handler: func(channel *Channel, msg CommandMsg) error {
			op := channel.IsOp(msg.From())
			channel.Send(NewSystemMsg(channel.commands.Help(op), msg.From()))
			return nil
		},
	})

	c.Add(Command{
		Prefix: "/me",
		Handler: func(channel *Channel, msg CommandMsg) error {
			me := strings.TrimLeft(msg.body, "/me")
			if me == "" {
				me = " is at a loss for words."
			} else {
				me = me[1:]
			}

			channel.Send(NewEmoteMsg(me, msg.From()))
			return nil
		},
	})

	c.Add(Command{
		Prefix: "/exit",
		Help:   "Exit the chat.",
		Handler: func(channel *Channel, msg CommandMsg) error {
			msg.From().Close()
			return nil
		},
	})
	c.Alias("/exit", "/quit")

	c.Add(Command{
		Prefix:     "/nick",
		PrefixHelp: "NAME",
		Help:       "Rename yourself.",
		Handler: func(channel *Channel, msg CommandMsg) error {
			args := msg.Args()
			if len(args) != 1 {
				return ErrMissingArg
			}
			u := msg.From()
			oldName := u.Name()
			u.SetName(args[0])

			body := fmt.Sprintf("%s is now known as %s.", oldName, u.Name())
			channel.Send(NewAnnounceMsg(body))
			return nil
		},
	})

	c.Add(Command{
		Prefix: "/names",
		Help:   "List users who are connected.",
		Handler: func(channel *Channel, msg CommandMsg) error {
			// TODO: colorize
			names := channel.NamesPrefix("")
			body := fmt.Sprintf("%d connected: %s", len(names), strings.Join(names, ", "))
			channel.Send(NewSystemMsg(body, msg.From()))
			return nil
		},
	})
	c.Alias("/names", "/list")

	c.Add(Command{
		Prefix:     "/theme",
		PrefixHelp: "[mono|colors]",
		Help:       "Set your color theme.",
		Handler: func(channel *Channel, msg CommandMsg) error {
			user := msg.From()
			args := msg.Args()
			if len(args) == 0 {
				theme := "plain"
				if user.Config.Theme != nil {
					theme = user.Config.Theme.Id()
				}
				body := fmt.Sprintf("Current theme: %s", theme)
				channel.Send(NewSystemMsg(body, user))
				return nil
			}

			id := args[0]
			for _, t := range Themes {
				if t.Id() == id {
					user.Config.Theme = &t
					body := fmt.Sprintf("Set theme: %s", id)
					channel.Send(NewSystemMsg(body, user))
					return nil
				}
			}
			return errors.New("theme not found")
		},
	})

	c.Add(Command{
		Op:         true,
		Prefix:     "/op",
		PrefixHelp: "USER",
		Help:       "Mark user as admin.",
		Handler: func(channel *Channel, msg CommandMsg) error {
			if !channel.IsOp(msg.From()) {
				return errors.New("must be op")
			}

			args := msg.Args()
			if len(args) != 1 {
				return errors.New("must specify user")
			}

			// TODO: Add support for fingerprint-based op'ing. This will
			// probably need to live in host land.
			member, err := channel.members.Get(Id(args[0]))
			if err != nil {
				return errors.New("user not found")
			}

			member.(*Member).Op = true
			return nil
		},
	})

	defaultCommands = &c
}
