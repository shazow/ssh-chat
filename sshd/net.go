package sshd

import (
	"net"
	"time"

	"github.com/shazow/rateio"
	"golang.org/x/crypto/ssh"
)

// Container for the connection and ssh-related configuration
type SSHListener struct {
	net.Listener
	config    *ssh.ServerConfig
	RateLimit func() rateio.Limiter
}

// Make an SSH listener socket
func ListenSSH(laddr string, config *ssh.ServerConfig) (*SSHListener, error) {
	socket, err := net.Listen("tcp", laddr)
	if err != nil {
		return nil, err
	}
	l := SSHListener{Listener: socket, config: config}
	return &l, nil
}

func (l *SSHListener) handleConn(conn net.Conn, stop <-chan bool) (*Terminal, error) {
	if l.RateLimit != nil {
		// TODO: Configurable Limiter?
		conn = ReadLimitConn(conn, l.RateLimit())
	}

	// Upgrade TCP connection to SSH connection
	sshConn, channels, requests, err := ssh.NewServerConn(conn, l.config)
	if err != nil {
		return nil, err
	}

	// FIXME: Disconnect if too many faulty requests? (Avoid DoS.)
	go ssh.DiscardRequests(requests)
	terminal, err := NewSession(sshConn, channels)
	if err != nil {
		return nil, err
	}
	go KeepAlive(terminal, 2, stop)
	return terminal, err
}

// Accept incoming connections as terminal requests and yield them
func (l *SSHListener) ServeTerminal() <-chan *Terminal {
	stop := make(chan bool)
	ch := make(chan *Terminal)

	go func() {
		defer l.Close()
		defer close(ch)

		for {
			conn, err := l.Accept()

			if err != nil {
				logger.Printf("Failed to accept connection: %v", err)
				return
			}

			// Goroutineify to resume accepting sockets early
			go func() {
				term, err := l.handleConn(conn, stop)
				if err != nil {
					logger.Printf("Failed to handshake: %v", err)
					return
				}
				ch <- term
			}()
		}
	}()

	return ch
}

// KeepAlive Setup a new keepalive goroutine
func KeepAlive(t *Terminal, interval time.Duration, stop <-chan bool) {
	// this sends keepalive packets every 2 seconds
	// there's no useful response from these, so we can just abort if there's an error
	tick := time.NewTicker(interval * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			_, err := t.Channel.SendRequest("keepalive", true, nil)
			if err != nil {
				return
			}
		case <-stop:
			return
		}
	}
}
