package main

import (
	"fmt"
	"net"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

type Clients map[string]*Client

type Server struct {
	sshConfig *ssh.ServerConfig
	sshSigner *ssh.Signer
	done      chan struct{}
	clients   Clients
	lock      sync.Mutex
	count     int
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
		clients:   Clients{},
		count:     0,
	}

	return &server, nil
}

func (s *Server) Broadcast(msg string, except *Client) {
	logger.Debugf("Broadcast to %d: %s", len(s.clients), strings.TrimRight(msg, "\r\n"))

	for _, client := range s.clients {
		if except != nil && client == except {
			continue
		}
		client.Msg <- msg
	}
}

func (s *Server) Add(client *Client) {
	client.Msg <- fmt.Sprintf("-> Welcome to ssh-chat. Enter /help for more.\r\n")

	s.lock.Lock()
	s.count++

	_, collision := s.clients[client.Name]
	if collision {
		newName := fmt.Sprintf("Guest%d", s.count)
		client.Msg <- fmt.Sprintf("-> Your name '%s' was taken, renamed to '%s'. Use /nick <name> to change it.\r\n", client.Name, newName)
		client.Name = newName
	}

	s.clients[client.Name] = client
	num := len(s.clients)
	s.lock.Unlock()

	s.Broadcast(fmt.Sprintf("* %s joined. (Total connected: %d)\r\n", client.Name, num), nil)
}

func (s *Server) Remove(client *Client) {
	s.lock.Lock()
	delete(s.clients, client.Name)
	s.lock.Unlock()

	s.Broadcast(fmt.Sprintf("* %s left.\r\n", client.Name), nil)
}

func (s *Server) Rename(client *Client, newName string) {
	s.lock.Lock()

	_, collision := s.clients[newName]
	if collision {
		client.Msg <- fmt.Sprintf("-> %s is not available.\r\n", newName)
		s.lock.Unlock()
		return
	}
	delete(s.clients, client.Name)
	oldName := client.Name
	client.Rename(newName)
	s.clients[client.Name] = client
	s.lock.Unlock()

	s.Broadcast(fmt.Sprintf("* %s is now known as %s.\r\n", oldName, newName), nil)
}

func (s *Server) List(prefix *string) []string {
	r := []string{}

	for name, _ := range s.clients {
		if prefix != nil && !strings.HasPrefix(name, *prefix) {
			continue
		}
		r = append(r, name)
	}

	return r
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

				client := NewClient(s, sshConn)
				s.Add(client)

				go func() {
					// Block until done, then remove.
					sshConn.Wait()
					s.Remove(client)
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
	for _, client := range s.clients {
		client.Conn.Close()
	}

	close(s.done)
}
