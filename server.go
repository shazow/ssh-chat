package main

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

const MAX_NAME_LENGTH = 32
const HISTORY_LEN = 20

var RE_STRIP_NAME = regexp.MustCompile("[[:^alpha:]]")

type Clients map[string]*Client

type Server struct {
	sshConfig *ssh.ServerConfig
	sshSigner *ssh.Signer
	done      chan struct{}
	clients   Clients
	lock      sync.Mutex
	count     int
	history   *History
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
			// fingerprint := md5.Sum(key.Marshal())
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
		history:   NewHistory(HISTORY_LEN),
	}

	return &server, nil
}

func (s *Server) Len() int {
	return len(s.clients)
}

func (s *Server) Broadcast(msg string, except *Client) {
	logger.Debugf("Broadcast to %d: %s", s.Len(), msg)
	s.history.Add(msg)

	for _, client := range s.clients {
		if except != nil && client == except {
			continue
		}
		client.Msg <- msg
	}
}

func (s *Server) Add(client *Client) {
	go func() {
		client.WriteLines(s.history.Get(10))
		client.Write(fmt.Sprintf("-> Welcome to ssh-chat. Enter /help for more."))
	}()

	s.lock.Lock()
	s.count++

	newName, err := s.proposeName(client.Name)
	if err != nil {
		client.Msg <- fmt.Sprintf("-> Your name '%s' is not available, renamed to '%s'. Use /nick <name> to change it.", client.Name, newName)
	}

	client.Rename(newName)
	s.clients[client.Name] = client
	num := len(s.clients)
	s.lock.Unlock()

	s.Broadcast(fmt.Sprintf("* %s joined. (Total connected: %d)", client.Name, num), client)
}

func (s *Server) Remove(client *Client) {
	s.lock.Lock()
	delete(s.clients, client.Name)
	s.lock.Unlock()

	s.Broadcast(fmt.Sprintf("* %s left.", client.Name), nil)
}

func (s *Server) proposeName(name string) (string, error) {
	// Assumes caller holds lock.
	var err error
	name = RE_STRIP_NAME.ReplaceAllString(name, "")

	if len(name) > MAX_NAME_LENGTH {
		name = name[:MAX_NAME_LENGTH]
	} else if len(name) == 0 {
		name = fmt.Sprintf("Guest%d", s.count)
	}

	_, collision := s.clients[name]
	if collision {
		err = fmt.Errorf("%s is not available.", name)
		name = fmt.Sprintf("Guest%d", s.count)
	}

	return name, err
}

func (s *Server) Rename(client *Client, newName string) {
	s.lock.Lock()

	newName, err := s.proposeName(newName)
	if err != nil {
		client.Msg <- fmt.Sprintf("-> %s", err)
		s.lock.Unlock()
		return
	}

	delete(s.clients, client.Name)
	oldName := client.Name
	client.Rename(newName)
	s.clients[client.Name] = client
	s.lock.Unlock()

	s.Broadcast(fmt.Sprintf("* %s is now known as %s.", oldName, newName), nil)
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

func (s *Server) Who(name string) *Client {
	return s.clients[name]
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

				logger.Infof("Connection #%d from: %s, %s, %s", s.count+1, sshConn.RemoteAddr(), sshConn.User(), sshConn.ClientVersion())

				go ssh.DiscardRequests(requests)

				client := NewClient(s, sshConn)
				client.handleChannels(channels)
				s.Add(client)

				go func() {
					// Block until done, then remove.
					sshConn.Wait()
					s.Remove(client)
				}()
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
