package sshd

import (
	"net"

	"golang.org/x/crypto/ssh"
)

// Container for the connection and ssh-related configuration
type SSHListener struct {
	net.Listener
	config *ssh.ServerConfig
}

// Make an SSH listener socket
func ListenSSH(laddr string, config *ssh.ServerConfig) (*SSHListener, error) {
	socket, err := net.Listen("tcp", laddr)
	if err != nil {
		return nil, err
	}
	l := SSHListener{socket, config}
	return &l, nil
}

func (l *SSHListener) handleConn(conn net.Conn) (*Terminal, error) {
	// Upgrade TCP connection to SSH connection
	sshConn, channels, requests, err := ssh.NewServerConn(conn, l.config)
	if err != nil {
		return nil, err
	}

	// FIXME: Disconnect if too many faulty requests? (Avoid DoS.)
	go ssh.DiscardRequests(requests)
	return NewSession(sshConn, channels)
}

// Accept incoming connections as terminal requests and yield them
func (l *SSHListener) ServeTerminal() <-chan *Terminal {
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
				term, err := l.handleConn(conn)
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
