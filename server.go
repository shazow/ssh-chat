package main

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

type Server struct {
	sshConfig *ssh.ServerConfig
	sshSigner *ssh.Signer
	done      chan struct{}
	clients   map[*Client]struct{}
	lock      sync.Mutex
}

func NewServer(privateKey []byte) (*Server, error) {
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, err
	}

	config := ssh.ServerConfig{
		NoClientAuth: false,
		PasswordCallback: func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			return nil, nil
		},
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			// fingerprint := md5.Sum(key.Marshal()
			return nil, nil
		},
	}
	config.AddHostKey(signer)

	server := Server{
		sshConfig: &config,
		sshSigner: &signer,
		done:      make(chan struct{}),
		clients:   map[*Client]struct{}{},
	}

	return &server, nil
}

func (s *Server) Broadcast(msg string, except *Client) {
	logger.Debugf("Broadcast to %d: %s", len(s.clients), strings.TrimRight(msg, "\r\n"))
	for client := range s.clients {
		if except != nil && client == except {
			continue
		}
		client.Msg <- msg
	}
}

func (s *Server) Start(laddr string) error {
	// Once a ServerConfig has been configured, connections can be
	// accepted.
	socket, err := net.Listen("tcp", laddr)
	if err != nil {
		return err
	}

	logger.Infof("Listening on %s", laddr)

	go func() {
		for {
			conn, err := socket.Accept()

			if err != nil {
				// TODO: Handle shutdown more gracefully?
				logger.Errorf("Failed to accept connection, aborting loop: %v", err)
				return
			}

			// Goroutineify to resume accepting sockets early.
			go func() {
				// From a standard TCP connection to an encrypted SSH connection
				sshConn, channels, requests, err := ssh.NewServerConn(conn, s.sshConfig)
				if err != nil {
					logger.Errorf("Failed to handshake: %v", err)
					return
				}

				logger.Infof("Connection from: %s, %s, %s", sshConn.RemoteAddr(), sshConn.User(), sshConn.ClientVersion())

				go ssh.DiscardRequests(requests)

				client := NewClient(s, sshConn, sshConn.User())
				// TODO: mutex this

				s.lock.Lock()
				s.clients[client] = struct{}{}
				num := len(s.clients)
				s.lock.Unlock()

				client.sendWelcome()

				s.Broadcast(fmt.Sprintf("* %s joined. (Total connected: %d)\r\n", client.Name, num), nil)

				go func() {
					sshConn.Wait()
					s.lock.Lock()
					delete(s.clients, client)
					s.lock.Unlock()

					s.Broadcast(fmt.Sprintf("* %s left.\r\n", client.Name), nil)
				}()

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

func (s *Server) Stop() {
	for client := range s.clients {
		client.Conn.Close()
	}

	close(s.done)
}
