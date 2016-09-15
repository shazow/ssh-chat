package chat

import (
	"time"

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

	SetName(string)

	Config() message.UserConfig
	SetConfig(message.UserConfig)

	Send(message.Message) error

	Joined() time.Time
	ReplyTo() message.Author
	SetReplyTo(message.Author)
	Prompt() string
	SetHighlight(string) error
}
