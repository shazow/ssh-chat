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
type Identity struct {
	sshd.Connection
	id      string
	chat    string
	created time.Time
}

// NewIdentity returns a new identity object from an sshd.Connection.
func NewIdentity(conn sshd.Connection) *Identity {
	return &Identity{
		Connection: conn,
		id:         chat.SanitizeName(conn.Name()),
		created:    time.Now(),
	}
}

func (i Identity) ID() string {
	return i.id
}

func (i *Identity) SetID(id string) {
	i.id = id
}

func (i *Identity) SetName(name string) {
	i.SetID(name)
}

func (i Identity) Name() string {
	return i.id
}

func (i Identity) Chat() string {
	return i.chat
}

func (i *Identity) SetChat(c string) {
	i.chat = c
}

// Whois returns a whois description for non-admin users.
func (i Identity) Whois() string {
	return "name: " + i.Name() + message.Newline +
		" > joined: " + humanize.Time(i.created)
}

// WhoisAdmin returns a whois description for admin users.
func (i Identity) WhoisAdmin() string {
	fingerprint := "(no public key)"
	if i.PublicKey() != nil {
		fingerprint = sshd.Fingerprint(i.PublicKey())
	}
	return "name: " + i.Name() + message.Newline +
		" > fingerprint: " + fingerprint + message.Newline +
		" > client: " + chat.SanitizeData(string(i.ClientVersion())) + message.Newline +
		" > joined: " + humanize.Time(i.created)
}

func (i Identity) WhoisMaster() string {
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
