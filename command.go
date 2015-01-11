package main

import (
	"errors"

	"github.com/shazow/ssh-chat/chat"
)

// InitCommands adds host-specific commands to a Commands container.
func InitCommands(h *Host, c *chat.Commands) {
	c.Add(chat.Command{
		Op:         true,
		Prefix:     "/msg",
		PrefixHelp: "USER MESSAGE",
		Help:       "Send MESSAGE to USER.",
		Handler: func(channel *chat.Channel, msg chat.CommandMsg) error {
			if !channel.IsOp(msg.From()) {
				return errors.New("must be op")
			}

			args := msg.Args()
			switch len(args) {
			case 0:
				return errors.New("must specify user")
			case 1:
				return errors.New("must specify message")
			}

			member, ok := channel.MemberById(chat.Id(args[0]))
			if !ok {
				return errors.New("user not found")
			}

			m := chat.NewPrivateMsg("hello", msg.From(), member.User)
			channel.Send(m)
			return nil
		},
	})
}
