package sshchat

import (
	"time"

	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/sshd"
)

type Client struct {
	user chat.Member
	conn sshd.Connection

	timestamp time.Time
}

type Replier interface {
	ReplyTo() message.Author
	SetReplyTo(message.Author)
}
