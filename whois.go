package sshchat

import (
	"net"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/sshd"
)

// Helpers for printing whois messages

type joinTimestamped interface {
	Joined() time.Time
}

func whoisPublic(clients []Client) string {
	// FIXME: Handle many clients
	conn, u := clients[0].conn, clients[0].user

	fingerprint := "(no public key)"
	if conn.PublicKey() != nil {
		fingerprint = sshd.Fingerprint(conn.PublicKey())
	}
	return "name: " + u.Name() + message.Newline +
		" > fingerprint: " + fingerprint + message.Newline +
		" > client: " + SanitizeData(string(conn.ClientVersion())) + message.Newline +
		" > joined: " + humanize.Time(u.(joinTimestamped).Joined())
}

func whoisAdmin(clients []Client) string {
	// FIXME: Handle many clients
	conn, u := clients[0].conn, clients[0].user

	ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	fingerprint := "(no public key)"
	if conn.PublicKey() != nil {
		fingerprint = sshd.Fingerprint(conn.PublicKey())
	}
	return "name: " + u.Name() + message.Newline +
		" > ip: " + ip + message.Newline +
		" > fingerprint: " + fingerprint + message.Newline +
		" > client: " + SanitizeData(string(conn.ClientVersion())) + message.Newline +
		" > joined: " + humanize.Time(u.(joinTimestamped).Joined())
}
