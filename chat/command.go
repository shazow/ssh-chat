package chat

import (
	"errors"
	"strings"
)

var ErrInvalidCommand = errors.New("invalid command")
var ErrNoOwner = errors.New("command without owner")

type CommandHandler func(c CommandMsg) error

type Commands map[string]CommandHandler

// Register command
func (c Commands) Add(command string, handler CommandHandler) {
	c[command] = handler
}

// Execute command message, assumes IsCommand was checked
func (c Commands) Run(msg CommandMsg) error {
	if msg.from == nil {
		return ErrNoOwner
	}

	handler, ok := c[msg.Command()]
	if !ok {
		return ErrInvalidCommand
	}

	return handler(msg)
}

var defaultCmdHandlers Commands

func init() {
	c := Commands{}

	c.Add("/me", func(msg CommandMsg) error {
		me := strings.TrimLeft(msg.body, "/me")
		if me == "" {
			me = " is at a loss for words."
		}

		// XXX: Finish this.

		return nil
	})

	defaultCmdHandlers = c
}
