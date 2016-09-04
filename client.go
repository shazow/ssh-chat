package sshchat

import (
	"net"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/sshd"
)

type Client struct {
	sshd.Connection
	message.User

	connected time.Time
}

// Whois returns a whois description for non-admin users.
func (client Client) Whois() string {
	conn, u := client.Connection, client.User
	fingerprint := "(no public key)"
	if conn.PublicKey() != nil {
		fingerprint = sshd.Fingerprint(conn.PublicKey())
	}
	return "name: " + u.Name() + message.Newline +
		" > fingerprint: " + fingerprint + message.Newline +
		" > client: " + SanitizeData(string(conn.ClientVersion())) + message.Newline +
		" > joined: " + humanize.Time(u.Joined())
}

// WhoisAdmin returns a whois description for admin users.
func (client Client) WhoisAdmin() string {
	conn, u := client.Connection, client.User
	ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	fingerprint := "(no public key)"
	if conn.PublicKey() != nil {
		fingerprint = sshd.Fingerprint(conn.PublicKey())
	}
	return "name: " + u.Name() + message.Newline +
		" > ip: " + ip + message.Newline +
		" > fingerprint: " + fingerprint + message.Newline +
		" > client: " + SanitizeData(string(conn.ClientVersion())) + message.Newline +
		" > joined: " + humanize.Time(u.Joined())
}
