package chat

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var ErrInvalidCommand = errors.New("invalid command")
var ErrNoOwner = errors.New("command without owner")
var ErrMissingArg = errors.New("missing argument")

type CommandHandler func(*Channel, CommandMsg) error

type Commands struct {
	handlers map[string]CommandHandler
	help     map[string]string
	sync.RWMutex
}

func NewCommands() *Commands {
	return &Commands{
		handlers: map[string]CommandHandler{},
		help:     map[string]string{},
	}
}

// Register command. If help string is empty, it will be hidden from Help().
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

// Execute command message, assumes IsCommand was checked.
func (c Commands) Run(channel *Channel, msg CommandMsg) error {
	if msg.from == nil {
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

	defaultCmdHandlers = c
}
