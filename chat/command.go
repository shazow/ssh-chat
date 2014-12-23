package chat

import (
	"errors"
	"strings"
)

var ErrInvalidCommand error = errors.New("invalid command")
var ErrNoOwner error = errors.New("command without owner")

type CmdHandler func(msg Message, args []string) error

type Commands map[string]CmdHandler

// Register command
func (c Commands) Add(cmd string, handler CmdHandler) {
	c[cmd] = handler
}

// Execute command message, assumes IsCommand was checked
func (c Commands) Run(msg Message) error {
	if msg.from == nil {
		return ErrNoOwner
	}

	cmd, args := msg.ParseCommand()
	handler, ok := c[cmd]
	if !ok {
		return ErrInvalidCommand
	}

	return handler(msg, args)
}

var defaultCmdHandlers Commands

func init() {
	c := Commands{}

	c.Add("me", func(msg Message, args []string) error {
		me := strings.TrimLeft(msg.Body, "/me")
		if me == "" {
			me = " is at a loss for words."
		}

		// XXX: Finish this.

		return nil
	})

	defaultCmdHandlers = c
}
