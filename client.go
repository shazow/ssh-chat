package sshchat

import (
	"time"

	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/sshd"
)

type Client struct {
	user *message.User
	conn sshd.Connection

	timestamp time.Time
}
