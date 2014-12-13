package main

import (
	"crypto/md5"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

const MAX_NAME_LENGTH = 32
const HISTORY_LEN = 20

var RE_STRIP_TEXT = regexp.MustCompile("[^0-9A-Za-z_]")

type Clients map[string]*Client

type Server struct {
	sshConfig *ssh.ServerConfig
	done      chan struct{}
	clients   Clients
	lock      sync.Mutex
	count     int
	history   *History
	admins    map[string]struct{}   // fingerprint lookup
	bannedPk  map[string]*time.Time // fingerprint lookup
	bannedIp  map[net.Addr]*time.Time
}

func NewServer(privateKey []byte) (*Server, error) {
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, err
	}

	server := Server{
		done:     make(chan struct{}),
		clients:  Clients{},
		count:    0,
		history:  NewHistory(HISTORY_LEN),
		admins:   map[string]struct{}{},
		bannedPk: map[string]*time.Time{},
		bannedIp: map[net.Addr]*time.Time{},
	}

	config := ssh.ServerConfig{
		NoClientAuth: false,
		// Auth-related things should be constant-time to avoid timing attacks.
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			fingerprint := Fingerprint(key)
			if server.IsBanned(fingerprint) {
				return nil, fmt.Errorf("Banned.")
			}
			ip := strings.Split(conn.RemoteAddr().String(), ":")[0]
			if ip == "73.3.250.197" {
				// Can't believe I'm doing this...
				return nil, fmt.Errorf("Banned.")
			}
			perm := &ssh.Permissions{Extensions: map[string]string{"fingerprint": fingerprint}}
			return perm, nil
		},
	}
	config.AddHostKey(signer)

	server.sshConfig = &config

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
		client.Msg <- fmt.Sprintf("-> Your name '%s' is not available, renamed to '%s'. Use /nick <name> to change it.", client.ColoredName(), ColorString(client.Color, newName))
	}

	client.Rename(newName)
	s.clients[client.Name] = client
	num := len(s.clients)
	s.lock.Unlock()

	s.Broadcast(fmt.Sprintf("* %s joined. (Total connected: %d)", client.ColoredName(), num), client)
}

func (s *Server) Remove(client *Client) {
	s.lock.Lock()
	delete(s.clients, client.Name)
	s.lock.Unlock()

	s.Broadcast(fmt.Sprintf("* %s left.", client.ColoredName()), nil)
}

func (s *Server) proposeName(name string) (string, error) {
	// Assumes caller holds lock.
	var err error
	name = RE_STRIP_TEXT.ReplaceAllString(name, "")

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

	// TODO: Use a channel/goroutine for adding clients, rathern than locks?
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

func (s *Server) Op(fingerprint string) {
	logger.Infof("Adding admin: %s", fingerprint)
	s.lock.Lock()
	s.admins[fingerprint] = struct{}{}
	s.lock.Unlock()
}

func (s *Server) IsOp(client *Client) bool {
	_, r := s.admins[client.Fingerprint()]
	return r
}

func (s *Server) IsBanned(fingerprint string) bool {
	ban, hasBan := s.bannedPk[fingerprint]
	if !hasBan {
		return false
	}
	if ban == nil {
		return true
	}
	if ban.Before(time.Now()) {
		s.Unban(fingerprint)
		return false
	}
	return true
}

func (s *Server) Ban(fingerprint string, duration *time.Duration) {
	var until *time.Time
	s.lock.Lock()
	if duration != nil {
		when := time.Now().Add(*duration)
		until = &when
	}
	s.bannedPk[fingerprint] = until
	s.lock.Unlock()
}

func (s *Server) Unban(fingerprint string) {
	s.lock.Lock()
	delete(s.bannedPk, fingerprint)
	s.lock.Unlock()
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
		defer socket.Close()
		for {
			conn, err := socket.Accept()

			if err != nil {
				logger.Errorf("Failed to accept connection: %v", err)
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
					logger.Errorf("Failed to handshake: %v", err)
					return
				}

				version := RE_STRIP_TEXT.ReplaceAllString(string(sshConn.ClientVersion()), "")
				if len(version) > 100 {
					version = "Evil Jerk with a superlong string"
				}
				logger.Infof("Connection #%d from: %s, %s, %s", s.count+1, sshConn.RemoteAddr(), sshConn.User(), version)

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

func (s *Server) Stop() {
	for _, client := range s.clients {
		client.Conn.Close()
	}

	close(s.done)
}

func Fingerprint(k ssh.PublicKey) string {
	hash := md5.Sum(k.Marshal())
	r := fmt.Sprintf("% x", hash)
	return strings.Replace(r, " ", ":", -1)
}
