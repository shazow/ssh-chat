package sshd

import (
	"net"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

// Server holds all the fields used by a server
type Server struct {
	sshConfig *ssh.ServerConfig
	done      chan struct{}
	started   time.Time
	sync.RWMutex
}

// Initialize a new server
func NewServer(privateKey []byte) (*Server, error) {
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, err
	}

	server := Server{
		done:    make(chan struct{}),
		started: time.Now(),
	}

	config := MakeNoAuth()
	config.AddHostKey(signer)

	server.sshConfig = config

	return &server, nil
}

// Start starts the server
func (s *Server) Start(laddr string) error {
	// Once a ServerConfig has been configured, connections can be
	// accepted.
	socket, err := net.Listen("tcp", laddr)
	if err != nil {
		return err
	}

	logger.Infof("Listening on %s", laddr)

	go func() {
		defer socket.Close()
		for {
			conn, err := socket.Accept()

			if err != nil {
				logger.Printf("Failed to accept connection: %v", err)
				if err == syscall.EINVAL {
					// TODO: Handle shutdown more gracefully?
					return
				}
			}

			// Goroutineify to resume accepting sockets early.
			go func() {
				// From a standard TCP connection to an encrypted SSH connection
				sshConn, channels, requests, err := ssh.NewServerConn(conn, s.sshConfig)
				if err != nil {
					logger.Printf("Failed to handshake: %v", err)
					return
				}

				go ssh.DiscardRequests(requests)

				client := NewClient(s, sshConn)
				go client.handleChannels(channels)
			}()
		}
	}()

	go func() {
		<-s.done
		socket.Close()
	}()

	return nil
}

// Stop stops the server
func (s *Server) Stop() {
	s.Lock()
	for _, client := range s.clients {
		client.Conn.Close()
	}
	s.Unlock()

	close(s.done)
}
