package chat

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// The error returned when an invalid command is issued.
var ErrInvalidCommand = errors.New("invalid command")

// The error returned when a command is given without an owner.
var ErrNoOwner = errors.New("command without owner")

// The error returned when a command is performed without the necessary number
// of arguments.
var ErrMissingArg = errors.New("missing argument")

// CommandHandler is the function signature for command handlers..
type CommandHandler func(*Channel, CommandMsg) error

// Commands is a registry of available commands.
type Commands struct {
	handlers map[string]CommandHandler
	help     map[string]string
	sync.RWMutex
}

// NewCommands returns a new Commands registry.
func NewCommands() *Commands {
	return &Commands{
		handlers: map[string]CommandHandler{},
		help:     map[string]string{},
	}
}

// Add will register a command. If help string is empty, it will be hidden from
// Help().
func (c Commands) Add(command string, help string, handler CommandHandler) {
	c.Lock()
	defer c.Unlock()

	c.handlers[command] = handler

	if help != "" {
		c.help[command] = help
	}
}

// Alias will add another command for the same handler, won't get added to help.
func (c Commands) Alias(command string, alias string) error {
	c.Lock()
	defer c.Unlock()

	handler, ok := c.handlers[command]
	if !ok {
		return ErrInvalidCommand
	}
	c.handlers[alias] = handler
	return nil
}

// Run executes a command message.
func (c Commands) Run(channel *Channel, msg CommandMsg) error {
	if msg.From == nil {
		return ErrNoOwner
	}

	c.RLock()
	defer c.RUnlock()

	handler, ok := c.handlers[msg.Command()]
	if !ok {
		return ErrInvalidCommand
	}

	return handler(channel, msg)
}

// Help will return collated help text as one string.
func (c Commands) Help() string {
	c.RLock()
	defer c.RUnlock()

	r := []string{}
	for cmd, line := range c.help {
		r = append(r, fmt.Sprintf("%s %s", cmd, line))
	}
	sort.Strings(r)

	return strings.Join(r, Newline)
}

var defaultCmdHandlers *Commands

func init() {
	c := NewCommands()

	c.Add("/help", "", func(channel *Channel, msg CommandMsg) error {
		channel.Send(NewSystemMsg("Available commands:"+Newline+c.Help(), msg.From()))
		return nil
	})

	c.Add("/me", "", func(channel *Channel, msg CommandMsg) error {
		me := strings.TrimLeft(msg.body, "/me")
		if me == "" {
			me = " is at a loss for words."
		} else {
			me = me[1:]
		}

		channel.Send(NewEmoteMsg(me, msg.From()))
		return nil
	})

	c.Add("/exit", "- Exit the chat.", func(channel *Channel, msg CommandMsg) error {
		msg.From().Close()
		return nil
	})
	c.Alias("/exit", "/quit")

	c.Add("/nick", "NAME - Rename yourself.", func(channel *Channel, msg CommandMsg) error {
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
	})

	c.Add("/names", "- List users who are connected.", func(channel *Channel, msg CommandMsg) error {
		// TODO: colorize
		names := channel.NamesPrefix("")
		body := fmt.Sprintf("%d connected: %s", len(names), strings.Join(names, ", "))
		channel.Send(NewSystemMsg(body, msg.From()))
		return nil
	})
	c.Alias("/names", "/list")

	c.Add("/theme", "[mono|colors] - Set your color theme.", func(channel *Channel, msg CommandMsg) error {
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
	})

	defaultCmdHandlers = c
}
