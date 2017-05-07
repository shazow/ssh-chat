package chat

import (
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/set"
)

// Member is a User with per-Room metadata attached to it.
type roomMember struct {
	Member
	Ignored *set.Set
}

type Member interface {
	message.Author

	Config() message.UserConfig
	SetConfig(message.UserConfig)

	Send(message.Message) error

	SetName(string)
}
