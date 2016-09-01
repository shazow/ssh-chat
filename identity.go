package sshchat

import (
	"net"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/sshd"
)

// Identity is a container for everything that identifies a client.
type identity struct {
	sshd.Connection
	id      string
	created time.Time
}

// Converts an sshd.Connection to an identity.
func toIdentity(conn sshd.Connection) *identity {
	return &identity{
		Connection: conn,
		id:         chat.SanitizeName(conn.Name()),
		created:    time.Now(),
	}
}

func (i identity) ID() string {
	return i.id
}

func (i *identity) SetName(name string) {
	i.id = chat.SanitizeName(name)
}

func (i identity) Name() string {
	return i.id
}

// Whois returns a whois description for non-admin users.
func (i identity) Whois() string {
	fingerprint := "(no public key)"
	if i.PublicKey() != nil {
		fingerprint = sshd.Fingerprint(i.PublicKey())
	}
	return "name: " + i.Name() + message.Newline +
		" > fingerprint: " + fingerprint + message.Newline +
		" > client: " + chat.SanitizeData(string(i.ClientVersion())) + message.Newline +
		" > joined: " + humanize.Time(i.created)
}

// WhoisAdmin returns a whois description for admin users.
func (i identity) WhoisAdmin() string {
	ip, _, _ := net.SplitHostPort(i.RemoteAddr().String())
	fingerprint := "(no public key)"
	if i.PublicKey() != nil {
		fingerprint = sshd.Fingerprint(i.PublicKey())
	}
	return "name: " + i.Name() + message.Newline +
		" > ip: " + ip + message.Newline +
		" > fingerprint: " + fingerprint + message.Newline +
		" > client: " + chat.SanitizeData(string(i.ClientVersion())) + message.Newline +
		" > joined: " + humanize.Time(i.created)
}
